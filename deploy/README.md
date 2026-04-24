# dipper-bot Deployment

## Linux (systemd user service)

Run the gateway as a systemd user service so it starts automatically and restarts on failure.

**1. Find the dipper-bot binary path:**

```bash
which dipper-bot   # e.g. /home/user/.local/bin/dipper-bot
```

**2. Copy the service file** to `~/.config/systemd/user/dipper-bot-gateway.service` and adjust `ExecStart` if needed:

```bash
mkdir -p ~/.config/systemd/user
cp deploy/dipper-bot-gateway.service ~/.config/systemd/user/dipper-bot-gateway.service
# Edit ExecStart if dipper-bot is not at ~/.local/bin/dipper-bot
```

**3. Enable and start:**

```bash
systemctl --user daemon-reload
systemctl --user enable --now dipper-bot-gateway
```

**Common operations:**

```bash
systemctl --user status dipper-bot-gateway        # check status
systemctl --user restart dipper-bot-gateway       # restart after config changes
journalctl --user -u dipper-bot-gateway -f        # follow logs
```

> **Note:** User services only run while you are logged in. To keep the gateway running after logout:
> ```bash
> loginctl enable-linger $USER
> ```

---

## Windows Service

### Option A: NSSM (Non-Sucking Service Manager)

1. Download [NSSM](https://nssm.cc/download)
2. Install the service:

```powershell
nssm install DipperBotGateway "C:\path\to\dipper-bot.exe" "gateway"
nssm set DipperBotGateway AppDirectory "C:\Users\YourUser\.dipper-bot"
nssm set DipperBotGateway AppStdout "C:\Users\YourUser\.dipper-bot\gateway.log"
nssm set DipperBotGateway AppStderr "C:\Users\YourUser\.dipper-bot\gateway.err"
nssm set DipperBotGateway AppRotateFiles 1
nssm set DipperBotGateway AppRotateBytes 1048576
nssm start DipperBotGateway
```

3. Manage the service:

```powershell
nssm status DipperBotGateway    # check status
nssm stop DipperBotGateway      # stop
nssm start DipperBotGateway     # start
nssm remove DipperBotGateway    # uninstall
```

### Option B: sc.exe (built-in)

```powershell
sc create DipperBotGateway binPath= "C:\path\to\dipper-bot.exe gateway" start= auto
sc start DipperBotGateway
sc stop DipperBotGateway
sc delete DipperBotGateway
```

> **Note:** With `sc`, stdout/stderr go to the Windows Event Log. Use NSSM for file-based logging.
