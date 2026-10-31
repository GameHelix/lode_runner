variables {
  ssh_public_keys = ["ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIKexampleexampleexampleexampleexample0000 you@laptop"]
  admin_cidrs     = ["203.0.113.7/32", "2001:db8::/64"]
}

run "renders_a_locked_down_host" {
  command = plan

  assert {
    condition     = strcontains(output.user_data, "PasswordAuthentication no") && strcontains(output.user_data, "PermitRootLogin no")
    error_message = "SSH must take keys only and refuse root."
  }
  assert {
    condition     = strcontains(output.user_data, "ufw allow from 203.0.113.7/32 to any port 22 proto tcp comment candylario-ssh") && strcontains(output.user_data, "ufw allow from 2001:db8::/64 to any port 22")
    error_message = "SSH must be open to each admin network."
  }
  assert {
    condition     = strcontains(output.user_data, "\n${var.ssh_public_keys[0]}\nKEYS\n")
    error_message = "The key must be authorized, one per line."
  }
  assert {
    condition     = !strcontains(output.user_data, "%%{") && strcontains(output.user_data, "user=deploy")
    error_message = "The template must render fully."
  }
}

run "refuses_a_network_that_is_not_one" {
  command = plan
  variables {
    admin_cidrs = ["everyone"]
  }
  expect_failures = [var.admin_cidrs]
}

run "refuses_a_private_key" {
  command = plan
  variables {
    ssh_public_keys = ["-----BEGIN OPENSSH PRIVATE KEY-----"]
  }
  expect_failures = [var.ssh_public_keys]
}
