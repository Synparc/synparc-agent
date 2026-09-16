# synparc-agent

> ðŸ¤– Agent lÃ©ger dÃ©ployÃ© sur les postes et serveurs Windows pour remonter les mÃ©triques systÃ¨me et les sessions actives vers le serveur central Synparc.

## Stack

- **Langage** : Go ou Rust (binaire natif, faible empreinte mÃ©moire)
- **Communication** : API REST JSON (HTTPS) vers `synparc-server`
- **Authentification** : Token d'enrÃ´lement unique par machine (UUID gÃ©nÃ©rÃ© Ã  l'enrÃ´lement)

## FonctionnalitÃ©s

| Feature | Description |
|---------|-------------|
| ðŸ“Š MÃ©triques systÃ¨me | CPU (%), RAM (utilisÃ©e/totale), espace disque |
| ðŸ‘¤ Sessions actives | Utilisateurs connectÃ©s, type de session (interactive, RDP, rÃ©seau) |
| ðŸ”‘ EnrÃ´lement | Auto-enrÃ´lement via token unique au premier dÃ©marrage |
| ðŸ”„ Auto-update | VÃ©rification des nouvelles versions via GitHub Releases |

## Architecture

```
[Machine Windows]
    â””â”€â”€ synparc-agent (service Windows)
            â”‚
            â”œâ”€â”€ Collecte mÃ©triques (CPU / RAM / Disk)
            â”œâ”€â”€ Lecture sessions actives (WTS API / logon events)
            â”‚
            â””â”€â”€ POST https://<server>/api/v1/agent/heartbeat
                    Authorization: Bearer <enrollment_token>
```

## Configuration

```toml
# synparc-agent.toml
server_url         = "https://synparc.example.com"
enrollment_token   = "xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx"
heartbeat_interval = 60   # secondes
```

## DÃ©veloppement local

```bash
# Go
go build -o synparc-agent .
./synparc-agent --config synparc-agent.toml

# Rust
cargo build --release
./target/release/synparc-agent --config synparc-agent.toml
```

## DÃ©ploiement

L'agent est distribuÃ© via `synparc-installer` (PowerShell).
Voir â†’ [synparc-installer](https://github.com/Synparc/synparc-installer).