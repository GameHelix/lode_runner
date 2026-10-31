# infra

Servers for Candylario in HCL for [OpenTofu](https://opentofu.org) (Terraform works too), at any cloud or on hardware you already have.

- `modules/host`: the provider-neutral bootstrap that every server runs.
- `existing`: runs the bootstrap over SSH on any server you have.
- `hetzner`: one Hetzner Cloud server that runs the bootstrap at first boot, with daily backups and cloud firewalls that let in SSH from `admin_cidrs` and HTTP(S) from anyone.

Another cloud gets its own small setup next to these. It only creates the machine and runs the same bootstrap at first boot.

Every server ends up the same:

- **Access:** a `deploy` user with your SSH keys. SSH takes keys only; root cannot log in.
- **Firewall:** SSH comes in from `admin_cidrs`, any address by default, and fail2ban watches it. Ports a service publishes, such as the app's HTTPS, are open to all.
- **Upkeep:** security updates install themselves, and the server gets swap.
- **Runtime:** Docker with Compose and log rotation.

## Use

```sh
cd existing
tofu init
tofu apply -var host=SERVER_IP -var 'ssh_public_keys=["ssh-ed25519 AAAA... you"]' -var 'admin_cidrs=["YOUR.IP/32"]'
```

The first run logs in as the provider's user, often root; from the second run on, add `-var user=deploy`.

At Hetzner, the state, the variables (`ssh_public_keys`, `admin_cidrs`) and the API token stay on the server in `/srv/secrets` (see Settings), and `hetzner/tofu.sh` runs OpenTofu here with them, deleting its copies afterwards:

```sh
./hetzner/tofu.sh deploy@91.99.222.102 plan
./hetzner/tofu.sh deploy@91.99.222.102 apply
```

The server is a cax11 (Arm, 2 vCPU, 4 GB) in Falkenstein; `-var location=hel1` and `-var server_type=...` change that. The very first apply, before the server exists, runs `tofu` in `hetzner/` directly with `HCLOUD_TOKEN` exported and the variables given with `-var`.

Then deploy the services over SSH from the repository root: `./deploy.sh trader|notifier|app deploy@SERVER_IP`.

## The server's address

The domain points at the server's IPv4 address, so it must outlive the server. `hetzner/main.tf` manages it as `hcloud_primary_ip.ipv4` with `auto_delete = false` and `prevent_destroy`: deleting the server leaves the address in the project, and no apply can delete it. The address references the server from its own resource, and the server's `public_net` block stays as it was at creation, because the provider powers a server off to change that block.

## Rebuilding the server

Not rehearsed yet; rehearse it on a spare server before relying on it, and write down how long each step took.

1. Make sure last night's backup exists (`./backup/restore.sh deploy@SERVER` lists the days), and copy `/srv/secrets` off the old server: it holds the settings, the age identity and the OpenTofu state.
2. In `hetzner/main.tf`, move the address reference to the server side, which only matters at creation: in `hcloud_primary_ip.ipv4` replace `assignee_type` and `assignee_id` with `location = var.location`; in `hcloud_server.host` add `ipv4 = hcloud_primary_ip.ipv4.id` to `public_net`.
3. `tofu apply -replace=hcloud_server.host`, with the state and token from that copy. The old server goes, the address stays, and the new server boots on it with the bootstrap; SSH host keys change, so remove the old entry from `~/.ssh/known_hosts`.
4. Put `/srv/secrets` back on the new server, deploy the three services (`./deploy.sh SERVICE deploy@SERVER`), then `./backup/install.sh deploy@SERVER` and `./backup/restore.sh deploy@SERVER DAY trader --live` for each service.
5. Check `https://candy.surf/healthz` shows every service and its commit, and that the bot answers `/help`. DNS needs no change.

`tofu -chdir=modules/host test` checks the bootstrap.

## Settings

Every setting lives on the server in one file, `/srv/secrets/candylario.env` (mode 600, nothing on any laptop): shared keys first, then `[trader]`, `[notifier]` and `[app]` sections for each service's own values, such as its database password, and `[backup]`. `HCLOUD_TOKEN` is there too, and `/srv/secrets/infra` holds the OpenTofu state and variables. `./env.sh SERVICE deploy@SERVER` prints one service's part, only the keys its `.env.example` lists, read over SSH; `deploy.sh` writes it to that service's `.env` on the server. To change a setting, edit the file on the server and deploy the services that use it.

## Backups

Every night `backup/backup.sh` on the server copies the three databases to S3-style object storage (Hetzner Object Storage), encrypted with [age](https://age-encryption.org) to `BACKUP_AGE_RECIPIENT`. The secret half, `BACKUP_AGE_IDENTITY`, is in the server's settings file, so a lost server also loses it: keep a copy of `/srv/secrets` somewhere else. Thirty days are kept, and a failed night emails the admin.

```sh
./backup/install.sh deploy@SERVER --now               # install, and back up once
./backup/restore.sh deploy@SERVER                     # the days stored
./backup/restore.sh deploy@SERVER DAY trader          # prove a backup in a scratch database
./backup/restore.sh deploy@SERVER DAY trader --live   # replace the live database
```

The `[backup]` section of the settings file holds `BACKUP_S3_ENDPOINT`, `BACKUP_S3_BUCKET`, `BACKUP_S3_ACCESS_KEY`, `BACKUP_S3_SECRET_KEY` and the age key pair.
