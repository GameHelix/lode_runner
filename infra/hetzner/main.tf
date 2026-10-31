# One Candylario server at Hetzner Cloud: a server that runs the bootstrap at
# first boot, kept by daily backups, behind cloud firewalls that let in SSH
# from the admin networks and web traffic from anyone. Services deploy onto it
# over SSH.

terraform {
  required_version = ">= 1.6"
  required_providers {
    hcloud = {
      source  = "hetznercloud/hcloud"
      version = "~> 1.50"
    }
  }
}

variable "hcloud_token" {
  description = "A Hetzner Cloud API token with write access; HCLOUD_TOKEN in the environment also works."
  type        = string
  sensitive   = true
  default     = ""
}

variable "name" {
  description = "The server's name at Hetzner."
  type        = string
  default     = "candylario"
}

variable "location" {
  description = "A Hetzner location outside the US, which Binance refuses."
  type        = string
  default     = "fsn1"
  validation {
    condition     = !contains(["ash", "hil"], var.location)
    error_message = "Binance refuses US addresses; pick fsn1, nbg1, hel1 or sin."
  }
}

variable "server_type" {
  description = "The server type."
  type        = string
  default     = "cax11"
}

variable "ssh_public_keys" {
  type = list(string)
}

variable "admin_cidrs" {
  description = "Networks allowed to reach SSH."
  type        = list(string)
  default     = ["0.0.0.0/0", "::/0"]
}

provider "hcloud" {
  token = var.hcloud_token != "" ? var.hcloud_token : null
}

# The server's own firewall lets SSH in from anywhere and the cloud firewall
# narrows it to admin_cidrs, so a new admin IP is one apply away and can never
# lock you out.
module "host" {
  source          = "../modules/host"
  ssh_public_keys = var.ssh_public_keys
}

resource "hcloud_ssh_key" "admin" {
  count      = length(var.ssh_public_keys)
  name       = "${var.name}-${count.index}"
  public_key = var.ssh_public_keys[count.index]
}

resource "hcloud_firewall" "ssh_only" {
  name = "${var.name}-ssh-only"
  rule {
    direction  = "in"
    protocol   = "tcp"
    port       = "22"
    source_ips = var.admin_cidrs
  }
}

# HTTP and HTTPS for the app, from anyone.
resource "hcloud_firewall" "web" {
  name = "${var.name}-web"
  dynamic "rule" {
    for_each = ["80", "443"]
    content {
      direction  = "in"
      protocol   = "tcp"
      port       = rule.value
      source_ips = ["0.0.0.0/0", "::/0"]
    }
  }
}

# The IPv4 address outlives the server: users' live Binance keys accept
# orders only from it, so a rebuilt server must come up on the same one. It
# was created with the server and imported on 2026-10-28; it is never
# deleted with the server, and never by an apply. To rebuild, see the README.
resource "hcloud_primary_ip" "ipv4" {
  name          = "${var.name}-ipv4"
  type          = "ipv4"
  assignee_type = "server"
  assignee_id   = hcloud_server.host.id
  auto_delete   = false
  labels        = { project = "candylario" }

  lifecycle {
    prevent_destroy = true
  }
}

resource "hcloud_server" "host" {
  name         = var.name
  server_type  = var.server_type
  location     = var.location
  image        = "ubuntu-24.04"
  ssh_keys     = hcloud_ssh_key.admin[*].id
  user_data    = module.host.user_data
  backups      = true
  firewall_ids = [hcloud_firewall.ssh_only.id, hcloud_firewall.web.id]
  labels       = { project = "candylario" }

  # Changing this block powers the server off (the provider unassigns and
  # assigns Primary IPs on a stopped server), so it stays as it was at
  # creation; the kept address references the server from its own
  # resource instead.
  public_net {
    ipv4_enabled = true
    ipv6_enabled = true
  }

  # The bootstrap and the keys count only at first boot; changing them later
  # must not replace the server and the data on it.
  lifecycle {
    ignore_changes = [user_data, ssh_keys]
  }
}

output "ip" {
  value = hcloud_server.host.ipv4_address
}

output "ssh" {
  value = "ssh deploy@${hcloud_server.host.ipv4_address}"
}
