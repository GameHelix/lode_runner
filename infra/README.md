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

At Hetzner, the state, the variables (`ssh_public_keys`, `admin_cidrs`) and the API token stay on the server in `/srv/secrets` (see Settings). `hetzner/tofu.sh` runs OpenTofu with them and deletes its copies afterwards; the **infra** workflow in GitHub Actions runs it, a plan by default and an apply with its box ticked. From a machine with SSH to the server it runs the same way:

```sh
./hetzner/tofu.sh deploy@91.99.222.102 plan
./hetzner/tofu.sh deploy@91.99.222.102 apply
```

The server is a cax11 (Arm, 2 vCPU, 4 GB) in Falkenstein; `-var location=hel1` and `-var server_type=...` change that. The very first apply, before the server exists, runs `tofu` in `hetzner/` directly with `HCLOUD_TOKEN` exported and the variables given with `-var`.

Then run `./actions-key.sh deploy@SERVER_IP` once, which gives GitHub Actions its SSH key to the server, and push: Actions deploys the services (`deploy.sh trader|notifier|app deploy@SERVER_IP` underneath).

## The server's address

The domain points at the server's IPv4 address, so it must outlive the server. `hetzner/main.tf` manages it as `hcloud_primary_ip.ipv4` with `auto_delete = false` and `prevent_destroy`: deleting the server leaves the address in the project, and no apply can delete it. The address references the server from its own resource, and the server's `public_net` block stays as it was at creation, because the provider powers a server off to change that block.

## Rebuilding the server

The **rebuild** workflow in GitHub Actions runs `hetzner/rebuild.sh`: plan shows what OpenTofu would change, and rebuild replaces the server.
1. It snapshots the server at Hetzner, then stops the services and backs them up to the backup volume. The site is down from here to the end.
2. OpenTofu replaces the server. The address and the volume move to the new one, which boots with the bootstrap and the keys in `ssh_public_keys`, the Actions key among them.
3. It puts `/srv/secrets` back from the volume, deploys the three services, installs the nightly backup and restores the databases.
4. It checks `/healthz` and prints the new SSH host key.

Rehearsed on 2026-10-31 (server 172581702 replaced by 173752005 on 91.99.222.102): 16.5 minutes end to end, 15 of them down. The steps took:
- snapshot: 40 s
- stop and back up: 8 s
- replace: 55 s
- first boot: 11 minutes, mostly packages and Docker
- settings: 4 s
- three deploys: 3 minutes
- restores: 26 s

Afterwards, set the `DEPLOY_KNOWN_HOSTS` secret to that key, run `ssh-keygen -R SERVER_IP` wherever else you SSH from, and delete the snapshot once the new server works.

If the old server is dead, the run cannot back it up: attach the volume to a server, take `/srv/secrets` from its newest day, and follow the same steps by hand.

`tofu -chdir=modules/host test` checks the bootstrap.

## Settings

Every setting lives on the server in one file, `/srv/secrets/candylario.env` (mode 600, nothing on any laptop): shared keys first, then `[trader]`, `[notifier]` and `[app]` sections for each service's own values, such as its database password, and `[backup]`. `HCLOUD_TOKEN` is there too, and `/srv/secrets/infra` holds the OpenTofu state and variables. `./env.sh SERVICE deploy@SERVER` prints one service's part, only the keys its `.env.example` lists, read over SSH; `deploy.sh` writes it to that service's `.env` on the server. To change a setting, edit the file on the server and deploy the services that use it.

## Backups

Every night `backup/backup.sh` on the server copies the three databases and `/srv/secrets` to the backup volume, a Hetzner volume mounted at `/mnt/backup` that outlives the server. Thirty days are kept, and a failed night emails the admin. The volume sits in the server's location, so an outage of the whole location takes both.

The **backup** workflow in GitHub Actions runs these: install, which also backs up once, or check, which proves the latest night of each database in a scratch database. From a machine with SSH to the server:

```sh
./backup/install.sh deploy@SERVER --now               # install, and back up once
./backup/restore.sh deploy@SERVER                     # the days stored
./backup/restore.sh deploy@SERVER DAY trader          # prove a backup in a scratch database
./backup/restore.sh deploy@SERVER DAY trader --live   # replace the live database
```

The `[backup]` section of the settings file holds `BACKUP_DIR` (`/mnt/backup`) and `BACKUP_DEVICE`, the volume's device (`tofu output backup_device`).
