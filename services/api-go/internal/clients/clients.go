// Package clients manages a Firm's ClientCompanies (spec section 5.4).
package clients

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/menem2024/uae-platform/services/api-go/internal/db"
	"github.com/menem2024/uae-platform/services/api-go/internal/db/sqlc"
)

// Error codes (the {"error": code} body).
const (
	CodeInvalidName    = "invalid_name"
	CodeInvalidNameAr  = "invalid_name_ar"
	CodeInvalidTRN     = "invalid_trn"
	CodeInvalidEmirate = "invalid_emirate"
	CodeTRNTaken       = "trn_taken"
)

// ErrTRNTaken is returned when another ClientCompany of the Firm has the TRN.
var ErrTRNTaken = errors.New(CodeTRNTaken)

// ValidationError carries the error code of the first invalid field.
type ValidationError struct{ Code string }

func (e ValidationError) Error() string { return e.Code }

var (
	trnRe    = regexp.MustCompile(`^[0-9]{15}$`)
	emirates = map[string]bool{"AUH": true, "DXB": true, "SHJ": true, "UAQ": true, "FUJ": true, "AJM": true, "RAK": true}
)

// Input is a create body, and a patch body with nil = keep. For trn, emirate
// and name_ar an empty string clears the value.
type Input struct {
	Name    *string `json:"name"`
	NameAr  *string `json:"name_ar"`
	TRN     *string `json:"trn"`
	Emirate *string `json:"emirate"`
}

// Fields is a validated, complete set of values.
type Fields struct {
	Name, NameAr, TRN, Emirate string
}

// View is the API representation. Documents counts are set only in lists.
type View struct {
	ID                   uuid.UUID `json:"id"`
	Name                 string    `json:"name"`
	NameAr               string    `json:"name_ar"`
	TRN                  *string   `json:"trn"`
	Emirate              *string   `json:"emirate"`
	Status               string    `json:"status"`
	DocumentsTotal       *int64    `json:"documents_total,omitempty"`
	DocumentsNeedsReview *int64    `json:"documents_needs_review,omitempty"`
	CreatedAt            time.Time `json:"created_at"`
	UpdatedAt            time.Time `json:"updated_at"`
}

func cleanText(s string) string {
	s = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, s)
	return strings.Join(strings.Fields(s), " ")
}

// Merge applies in over base (nil keeps) and validates the result.
func Merge(base Fields, in Input) (Fields, error) {
	out := base
	if in.Name != nil {
		out.Name = cleanText(*in.Name)
	}
	if in.NameAr != nil {
		out.NameAr = cleanText(*in.NameAr)
	}
	if in.TRN != nil {
		out.TRN = strings.TrimSpace(*in.TRN)
	}
	if in.Emirate != nil {
		out.Emirate = strings.ToUpper(strings.TrimSpace(*in.Emirate))
	}
	switch {
	case out.Name == "" || utf8.RuneCountInString(out.Name) > 200:
		return out, ValidationError{CodeInvalidName}
	case utf8.RuneCountInString(out.NameAr) > 200:
		return out, ValidationError{CodeInvalidNameAr}
	case out.TRN != "" && !trnRe.MatchString(out.TRN):
		return out, ValidationError{CodeInvalidTRN}
	case out.Emirate != "" && !emirates[out.Emirate]:
		return out, ValidationError{CodeInvalidEmirate}
	}
	return out, nil
}

// ListFilter selects a page of ClientCompanies.
type ListFilter struct {
	Status    string // active | archived | all
	Query     string
	Limit     int
	AfterName string
	AfterID   uuid.UUID
}

// Store persists ClientCompanies; every method runs inside db.WithFirm.
type Store interface {
	List(ctx context.Context, firmID uuid.UUID, f ListFilter) ([]View, error)
	Create(ctx context.Context, firmID uuid.UUID, f Fields) (View, error)
	Get(ctx context.Context, firmID, id uuid.UUID) (View, error)
	Update(ctx context.Context, firmID, id uuid.UUID, in Input) (View, error)
	SetStatus(ctx context.Context, firmID, id uuid.UUID, status string) (View, error)
}

// PGStore implements Store over Postgres.
type PGStore struct{ Pool *pgxpool.Pool }

var _ Store = PGStore{}

func text(s string) pgtype.Text { return pgtype.Text{String: s, Valid: s != ""} }

func ptr(t pgtype.Text) *string {
	if !t.Valid {
		return nil
	}
	return &t.String
}

func view(c sqlc.ClientCompany) View {
	return View{ID: c.ID, Name: c.Name, NameAr: c.NameAr, TRN: ptr(c.Trn), Emirate: ptr(c.Emirate), Status: c.Status,
		CreatedAt: c.CreatedAt.Time, UpdatedAt: c.UpdatedAt.Time}
}

func mapUnique(err error) error {
	var pg *pgconn.PgError
	if errors.As(err, &pg) && pg.Code == "23505" && pg.ConstraintName == "client_companies_firm_trn_uniq" {
		return ErrTRNTaken
	}
	return err
}

// List returns up to f.Limit items ordered by (name, id).
func (s PGStore) List(ctx context.Context, firmID uuid.UUID, f ListFilter) ([]View, error) {
	var out []View
	err := db.WithFirm(ctx, s.Pool, firmID, func(q *sqlc.Queries) error {
		p := sqlc.ListClientCompaniesParams{Status: f.Status, PageLimit: int32(f.Limit)} //nolint:gosec // limit <= 101
		if f.Query != "" {
			p.Pattern = pgtype.Text{String: f.Query, Valid: true}
		}
		if f.AfterID != uuid.Nil {
			p.AfterName = pgtype.Text{String: f.AfterName, Valid: true}
			p.AfterID = uuid.NullUUID{UUID: f.AfterID, Valid: true}
		}
		rows, err := q.ListClientCompanies(ctx, p)
		if err != nil {
			return fmt.Errorf("list client companies: %w", err)
		}
		for _, r := range rows {
			v := view(sqlc.ClientCompany{ID: r.ID, FirmID: r.FirmID, Name: r.Name, NameAr: r.NameAr, Trn: r.Trn,
				Tin: r.Tin, Emirate: r.Emirate, Status: r.Status, CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt})
			total, review := r.DocumentsTotal, r.DocumentsNeedsReview
			v.DocumentsTotal, v.DocumentsNeedsReview = &total, &review
			out = append(out, v)
		}
		return nil
	})
	return out, err
}

// Create inserts an active ClientCompany; ErrTRNTaken on a duplicate TRN.
func (s PGStore) Create(ctx context.Context, firmID uuid.UUID, f Fields) (View, error) {
	var v View
	err := db.WithFirm(ctx, s.Pool, firmID, func(q *sqlc.Queries) error {
		c, err := q.CreateClientCompany(ctx, sqlc.CreateClientCompanyParams{FirmID: firmID, Name: f.Name,
			NameAr: f.NameAr, Trn: text(f.TRN), Emirate: text(f.Emirate)})
		if err != nil {
			return mapUnique(err)
		}
		v = view(c)
		return nil
	})
	return v, err
}

// Get returns one ClientCompany or db.ErrNotFound.
func (s PGStore) Get(ctx context.Context, firmID, id uuid.UUID) (View, error) {
	var v View
	err := db.WithFirm(ctx, s.Pool, firmID, func(q *sqlc.Queries) error {
		c, err := q.GetClientCompany(ctx, id)
		if err != nil {
			return err
		}
		v = view(c)
		return nil
	})
	return v, err
}

// Update merges in over the current row in one transaction. UpdateClientCompany
// replaces the whole row (like trn), so tin -- which Input/Fields never carries --
// must be re-sent from the row just read, or the PATCH would silently clear it.
func (s PGStore) Update(ctx context.Context, firmID, id uuid.UUID, in Input) (View, error) {
	var v View
	err := db.WithFirm(ctx, s.Pool, firmID, func(q *sqlc.Queries) error {
		cur, err := q.GetClientCompany(ctx, id)
		if err != nil {
			return err
		}
		f, err := Merge(Fields{Name: cur.Name, NameAr: cur.NameAr, TRN: cur.Trn.String, Emirate: cur.Emirate.String}, in)
		if err != nil {
			return err
		}
		c, err := q.UpdateClientCompany(ctx, sqlc.UpdateClientCompanyParams{ID: id, Name: f.Name, NameAr: f.NameAr,
			Trn: text(f.TRN), Tin: cur.Tin, Emirate: text(f.Emirate)})
		if err != nil {
			return mapUnique(err)
		}
		v = view(c)
		return nil
	})
	return v, err
}

// SetStatus archives or restores a ClientCompany.
func (s PGStore) SetStatus(ctx context.Context, firmID, id uuid.UUID, status string) (View, error) {
	var v View
	err := db.WithFirm(ctx, s.Pool, firmID, func(q *sqlc.Queries) error {
		c, err := q.SetClientCompanyStatus(ctx, sqlc.SetClientCompanyStatusParams{ID: id, Status: status})
		if err != nil {
			return err
		}
		v = view(c)
		return nil
	})
	return v, err
}
