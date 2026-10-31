# The provider-neutral part of a Candylario server: one bootstrap script that
# every provider runs at first boot as user data, and that the "existing"
# setup runs over SSH.

terraform {
  required_version = ">= 1.6"
}

variable "ssh_public_keys" {
  description = "OpenSSH public keys that may log in as the deploy user."
  type        = list(string)
  validation {
    condition     = length(var.ssh_public_keys) > 0 && alltrue([for k in var.ssh_public_keys : can(regex("^(ssh|ecdsa|sk)-[a-z0-9@.-]+ [A-Za-z0-9+/=]+( [^\\n]*)?$", k))])
    error_message = "Give at least one OpenSSH public key, one per entry."
  }
}

variable "admin_cidrs" {
  description = "Networks allowed to reach SSH. Narrow it to your own IP."
  type        = list(string)
  default     = ["0.0.0.0/0", "::/0"]
  validation {
    condition     = length(var.admin_cidrs) > 0 && alltrue([for c in var.admin_cidrs : can(cidrhost(c, 0))])
    error_message = "admin_cidrs must be CIDR blocks, such as 203.0.113.7/32."
  }
}

variable "deploy_user" {
  description = "The user that runs the services."
  type        = string
  default     = "deploy"
  validation {
    condition     = can(regex("^[a-z][a-z0-9-]{0,30}$", var.deploy_user))
    error_message = "deploy_user must be a lowercase Unix user name."
  }
}

variable "swap_gb" {
  description = "Swap to add when the server has none; 0 for none."
  type        = number
  default     = 2
  validation {
    condition     = var.swap_gb >= 0 && floor(var.swap_gb) == var.swap_gb
    error_message = "swap_gb must be a whole number of gigabytes."
  }
}

output "user_data" {
  description = "The bootstrap script, as user data or to run over SSH."
  value = templatefile("${path.module}/bootstrap.sh.tftpl", {
    deploy_user     = var.deploy_user
    ssh_public_keys = var.ssh_public_keys
    admin_cidrs     = var.admin_cidrs
    swap_gb         = var.swap_gb
  })
}
