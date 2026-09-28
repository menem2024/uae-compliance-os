terraform {
  required_version = ">= 1.13"

  required_providers {
    hcloud = {
      source  = "hetznercloud/hcloud"
      version = "~> 1.69"
    }
    cloudflare = {
      source  = "cloudflare/cloudflare"
      version = "~> 5.26"
    }
  }
}

provider "hcloud" {
  token = var.hcloud_token
}

provider "cloudflare" {
  api_token = var.cloudflare_api_token
}

# Cloudflare's published IPv4 ranges (https://www.cloudflare.com/ips-v4/), the
# only source allowed to reach 443: staging terminates TLS behind the
# Cloudflare proxy, so the origin only needs to accept Cloudflare's edge.
locals {
  cloudflare_ipv4_cidrs = [
    "173.245.48.0/20",
    "103.21.244.0/22",
    "103.22.200.0/22",
    "103.31.4.0/22",
    "141.101.64.0/18",
    "108.162.192.0/18",
    "190.93.240.0/20",
    "188.114.96.0/20",
    "197.234.240.0/22",
    "198.41.128.0/17",
    "162.158.0.0/15",
    "104.16.0.0/13",
    "104.24.0.0/14",
    "172.64.0.0/13",
    "131.0.72.0/22",
  ]
}

resource "hcloud_server" "app" {
  name        = "compliance-app"
  server_type = "cx22"
  image       = "ubuntu-24.04"
  location    = "fsn1"
  user_data   = file("${path.module}/cloud-init.yaml")

  firewall_ids = [hcloud_firewall.app.id]

  labels = {
    project = "compliance-os"
    phase   = "0"
  }
}

resource "hcloud_firewall" "app" {
  name = "compliance-app"

  # SSH: admin only.
  rule {
    direction  = "in"
    protocol   = "tcp"
    port       = "22"
    source_ips = [var.admin_cidr]
  }

  # HTTPS: Cloudflare's edge only (TLS terminates there).
  rule {
    direction  = "in"
    protocol   = "tcp"
    port       = "443"
    source_ips = local.cloudflare_ipv4_cidrs
  }

  # k3s API: admin only.
  rule {
    direction  = "in"
    protocol   = "tcp"
    port       = "6443"
    source_ips = [var.admin_cidr]
  }
}

resource "cloudflare_dns_record" "app" {
  zone_id = var.cloudflare_zone_id
  name    = "app.${var.domain}"
  type    = "A"
  content = hcloud_server.app.ipv4_address
  ttl     = 1 # automatic when proxied
  proxied = true
}

resource "cloudflare_dns_record" "auth" {
  zone_id = var.cloudflare_zone_id
  name    = "auth.${var.domain}"
  type    = "A"
  content = hcloud_server.app.ipv4_address
  ttl     = 1 # automatic when proxied
  proxied = true
}
