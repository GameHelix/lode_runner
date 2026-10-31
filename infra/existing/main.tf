# Prepares a server you already have, at any provider or on your own
# hardware, by running the bootstrap over SSH. It runs again whenever the
# bootstrap changes. The first run logs in as the provider's user (often
# root); after it, root cannot log in, so later runs use user = "deploy".

terraform {
  required_version = ">= 1.6"
}

variable "host" {
  description = "The server's address."
  type        = string
}

variable "user" {
  description = "The SSH user: the provider's user the first time, then deploy."
  type        = string
  default     = "root"
}

variable "private_key_path" {
  description = "A private key file to log in with; empty to use your SSH agent."
  type        = string
  default     = ""
}

variable "ssh_public_keys" {
  type = list(string)
}

variable "admin_cidrs" {
  description = "Networks allowed to reach SSH."
  type        = list(string)
  default     = ["0.0.0.0/0", "::/0"]
}

module "host" {
  source          = "../modules/host"
  ssh_public_keys = var.ssh_public_keys
  admin_cidrs     = var.admin_cidrs
}

resource "terraform_data" "bootstrap" {
  triggers_replace = [var.host, module.host.user_data]

  connection {
    type        = "ssh"
    host        = var.host
    user        = var.user
    agent       = var.private_key_path == ""
    private_key = var.private_key_path == "" ? null : file(pathexpand(var.private_key_path))
  }

  provisioner "file" {
    content     = module.host.user_data
    destination = "/tmp/candylario-bootstrap.sh"
  }

  provisioner "remote-exec" {
    inline = [
      "if [ \"$(id -u)\" -eq 0 ]; then bash /tmp/candylario-bootstrap.sh; else sudo bash /tmp/candylario-bootstrap.sh; fi",
      "rm -f /tmp/candylario-bootstrap.sh",
    ]
  }
}

output "ssh" {
  value = "ssh deploy@${var.host}"
}
