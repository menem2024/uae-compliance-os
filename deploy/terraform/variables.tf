# Compliance OS staging skeleton: one Hetzner Cloud server running k3s, DNS
# through Cloudflare. Validate only (`terraform init -backend=false &&
# terraform validate`) — never apply, never plan against real providers.
# See the Global Constraints in the Phase 0 plan.

variable "hcloud_token" {
  description = "Hetzner Cloud API token."
  type        = string
  sensitive   = true
}

variable "cloudflare_api_token" {
  description = "Cloudflare API token, scoped to DNS edit on cloudflare_zone_id."
  type        = string
  sensitive   = true
}

variable "cloudflare_zone_id" {
  description = "Cloudflare zone ID for var.domain."
  type        = string
}

variable "admin_cidr" {
  description = "CIDR allowed to reach SSH (22) and the k3s API (6443), e.g. \"203.0.113.4/32\"."
  type        = string
}

variable "domain" {
  description = "Base domain; the app and auth DNS records are app.<domain> and auth.<domain>."
  type        = string
}
