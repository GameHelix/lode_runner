# Renders the bootstrap with sample values, for shellcheck in CI.
module "host" {
  source          = "../.."
  ssh_public_keys = ["ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIKexampleexampleexampleexampleexample0000 ci"]
}

output "user_data" {
  value = module.host.user_data
}
