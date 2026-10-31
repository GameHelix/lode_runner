# One Candylario server at Hetzner Cloud: a server that runs the bootstrap at
# first boot, with a backup volume for the nightly backups, behind cloud
# firewalls that let in SSH from the admin networks and web traffic from
# anyone. Services deploy onto it over SSH.

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
  description = "The Hetzner location."
  type        = string
  default     = "fsn1"
}

variable "server_type" {
  description = "The server type."
  type        = string
  default     = "cx23"
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

# The IPv4 address outlives the server: the domain points at it, so a rebuilt
# server comes up on the same one. It was created with the server and
# imported on 2026-10-28; it is never deleted with the server, and never by an
# apply. The server takes it at creation; to rebuild, see the README.
resource "hcloud_primary_ip" "ipv4" {
  name        = "${var.name}-ipv4"
  type        = "ipv4"
  location    = var.location
  auto_delete = false
  labels      = { project = "candylario" }

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
  backups      = false # the backup volume and the rebuild workflow cover it
  firewall_ids = [hcloud_firewall.ssh_only.id, hcloud_firewall.web.id]
  labels       = { project = "candylario" }

  # Changing this block powers the server off (the provider unassigns and
  # assigns Primary IPs on a stopped server), so it changes only with a
  # rebuild, which creates the server on the kept address.
  public_net {
    ipv4_enabled = true
    ipv4         = hcloud_primary_ip.ipv4.id
    ipv6_enabled = true
  }

  # The bootstrap and the keys count only at first boot; changing them later
  # must not replace the server and the data on it.
  lifecycle {
    ignore_changes = [user_data, ssh_keys]
  }
}

# The nightly backups land on this volume, a disk of its own that outlives
# the server: a rebuilt server gets it attached and restores from it. It sits
# in the server's location, so it does not survive an outage of the whole
# location.
resource "hcloud_volume" "backup" {
  name              = "${var.name}-backup"
  size              = 10
  location          = var.location
  format            = "ext4"
  delete_protection = true
  labels            = { project = "candylario" }

  lifecycle {
    prevent_destroy = true
  }
}

resource "hcloud_volume_attachment" "backup" {
  volume_id = hcloud_volume.backup.id
  server_id = hcloud_server.host.id
}

output "ip" {
  value = hcloud_server.host.ipv4_address
}

output "backup_device" {
  value = hcloud_volume.backup.linux_device
}

output "ssh" {
  value = "ssh deploy@${hcloud_server.host.ipv4_address}"
}
