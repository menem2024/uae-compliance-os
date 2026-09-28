output "server_ipv4" {
  description = "Public IPv4 address of the k3s server."
  value       = hcloud_server.app.ipv4_address
}

output "server_id" {
  description = "Hetzner Cloud server ID."
  value       = hcloud_server.app.id
}

output "app_hostname" {
  description = "DNS name that resolves to the app (web) through Cloudflare."
  value       = cloudflare_dns_record.app.name
}

output "auth_hostname" {
  description = "DNS name that resolves to Zitadel through Cloudflare."
  value       = cloudflare_dns_record.auth.name
}
