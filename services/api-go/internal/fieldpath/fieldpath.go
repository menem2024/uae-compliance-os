// Package fieldpath reads and writes single string/bool fields of a canonical Invoice by the
// CI §12 path grammar: segment ("." segment)*, segment = proto_field_name ("[" index "]")?,
// 0-based indices, no "invoice." prefix. It is pure: no I/O, no clock, no globals that change.
package fieldpath

import (
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"

	compliancev1 "github.com/menem2024/uae-platform/services/api-go/gen/compliance/v1"
)

// FieldChange is one field edit. OldValue is the concurrency guard: it must equal the current
// value ("" when the field is absent) for the change to apply.
type FieldChange struct {
	Path     string `json:"path"`
	OldValue string `json:"old_value"`
	NewValue string `json:"new_value"`
}

var (
	// ErrBadPath: the path breaks the grammar, names no field, does not end on a string or bool
	// field, or indexes a list beyond its length.
	ErrBadPath = errors.New("fieldpath: bad path")
	// ErrOldValueMismatch: a change's OldValue differs from the current value.
	ErrOldValueMismatch = errors.New("fieldpath: old value mismatch")
	// ErrNoChanges: ApplyChanges was called with no changes.
	ErrNoChanges = errors.New("fieldpath: no changes")
)

type segment struct {
	name  string
	index int // -1 when the segment has no index
}

var segmentRE = regexp.MustCompile(`^([a-z][a-z0-9_]*)(?:\[([0-9]+)\])?$`)

func parse(path string) ([]segment, error) {
	if path == "" {
		return nil, fmt.Errorf("%w: empty path", ErrBadPath)
	}
	parts := strings.Split(path, ".")
	segs := make([]segment, 0, len(parts))
	for _, p := range parts {
		m := segmentRE.FindStringSubmatch(p)
		if m == nil {
			return nil, fmt.Errorf("%w: segment %q in %q", ErrBadPath, p, path)
		}
		s := segment{name: m[1], index: -1}
		if m[2] != "" {
			n, err := strconv.Atoi(m[2])
			if err != nil {
				return nil, fmt.Errorf("%w: index %q in %q", ErrBadPath, m[2], path)
			}
			s.index = n
		}
		segs = append(segs, s)
	}
	return segs, nil
}

// check validates segs against the Invoice descriptor and returns the leaf field.
func check(segs []segment, path string) (protoreflect.FieldDescriptor, error) {
	md := (&compliancev1.Invoice{}).ProtoReflect().Descriptor()
	for i, s := range segs {
		fd := md.Fields().ByName(protoreflect.Name(s.name))
		if fd == nil {
			return nil, fmt.Errorf("%w: no field %q in %q", ErrBadPath, s.name, path)
		}
		last := i == len(segs)-1
		if last {
			if fd.IsList() || fd.IsMap() || s.index >= 0 ||
				(fd.Kind() != protoreflect.StringKind && fd.Kind() != protoreflect.BoolKind) {
				return nil, fmt.Errorf("%w: %q does not end on a string or bool field", ErrBadPath, path)
			}
			return fd, nil
		}
		if fd.Kind() != protoreflect.MessageKind || fd.IsMap() {
			return nil, fmt.Errorf("%w: %q is not a message in %q", ErrBadPath, s.name, path)
		}
		if fd.IsList() != (s.index >= 0) {
			return nil, fmt.Errorf("%w: index use on %q in %q", ErrBadPath, s.name, path)
		}
		md = fd.Message()
	}
	return nil, fmt.Errorf("%w: %q", ErrBadPath, path)
}

// Get returns the value at path: "" when it (or a parent) is absent, and "true"/"false" for a bool
// on an existing message. It never mutates inv.
func Get(inv *compliancev1.Invoice, path string) (string, error) {
	segs, err := parse(path)
	if err != nil {
		return "", err
	}
	fd, err := check(segs, path)
	if err != nil {
		return "", err
	}
	msg := inv.ProtoReflect()
	for _, s := range segs[:len(segs)-1] {
		f := msg.Descriptor().Fields().ByName(protoreflect.Name(s.name))
		if f.IsList() {
			l := msg.Get(f).List()
			if s.index >= l.Len() {
				return "", nil
			}
			msg = l.Get(s.index).Message()
			continue
		}
		if !msg.Has(f) {
			return "", nil
		}
		msg = msg.Get(f).Message()
	}
	return format(msg.Get(fd), fd), nil
}

func format(v protoreflect.Value, fd protoreflect.FieldDescriptor) string {
	if fd.Kind() == protoreflect.BoolKind {
		return strconv.FormatBool(v.Bool())
	}
	return v.String()
}

// ApplyChanges checks each change's OldValue against the value at its path (after the preceding
// changes), then sets NewValue. A list index equal to the list length appends an element. It is
// atomic: on any error inv is left unchanged.
func ApplyChanges(inv *compliancev1.Invoice, changes []FieldChange) error {
	if len(changes) == 0 {
		return ErrNoChanges
	}
	work := proto.Clone(inv).(*compliancev1.Invoice)
	for _, c := range changes {
		if err := applyOne(work, c); err != nil {
			return err
		}
	}
	proto.Reset(inv)
	proto.Merge(inv, work)
	return nil
}

func applyOne(inv *compliancev1.Invoice, c FieldChange) error {
	segs, err := parse(c.Path)
	if err != nil {
		return err
	}
	fd, err := check(segs, c.Path)
	if err != nil {
		return err
	}
	cur, err := Get(inv, c.Path)
	if err != nil {
		return err
	}
	if cur != c.OldValue {
		return fmt.Errorf("%w: %s is %q, change expects %q", ErrOldValueMismatch, c.Path, cur, c.OldValue)
	}
	var val protoreflect.Value
	if fd.Kind() == protoreflect.BoolKind {
		switch c.NewValue {
		case "true":
			val = protoreflect.ValueOfBool(true)
		case "false":
			val = protoreflect.ValueOfBool(false)
		default:
			return fmt.Errorf("%w: %s is a bool, new value %q is not true or false", ErrBadPath, c.Path, c.NewValue)
		}
	} else {
		val = protoreflect.ValueOfString(c.NewValue)
	}
	msg := inv.ProtoReflect()
	for _, s := range segs[:len(segs)-1] {
		f := msg.Descriptor().Fields().ByName(protoreflect.Name(s.name))
		if !f.IsList() {
			msg = msg.Mutable(f).Message()
			continue
		}
		l := msg.Mutable(f).List()
		switch {
		case s.index < l.Len():
			msg = l.Get(s.index).Message()
		case s.index == l.Len():
			msg = l.AppendMutable().Message()
		default:
			return fmt.Errorf("%w: index %d beyond length %d in %q", ErrBadPath, s.index, l.Len(), c.Path)
		}
	}
	msg.Set(fd, val)
	return nil
}

var agentForbidden = map[string]struct{}{
	"invoice_number":                     {},
	"uuid":                               {},
	"issue_date":                         {},
	"seller_trn":                         {},
	"buyer_trn":                          {},
	"seller.tax_registration_identifier": {},
	"seller.electronic_address.id":       {},
	"buyer.electronic_address.id":        {},
	"seller.legal_registration.id":       {},
	"buyer.legal_registration.id":        {},
	"principal_id":                       {},
	"beneficiary_id":                     {},
	"tax_representative.vat_identifier":  {},
}

// AgentForbidden reports paths an agent may never change: identifiers a human must type.
// A human correction may change any path.
func AgentForbidden(path string) bool {
	_, ok := agentForbidden[path]
	return ok
}
