# synparc-agent

> 🤖 Agent léger déployé sur les postes et serveurs Windows pour remonter les métriques système et les sessions actives vers le serveur central Synparc.

## Stack

- **Langage** : Go ou Rust (binaire natif, faible empreinte mémoire)
- **Communication** : API REST JSON (HTTPS) vers `synparc-server`
- **Authentification** : Token d'enrôlement unique par machine (UUID généré à l'enrôlement)

## Fonctionnalités

| Feature | Description |
|---------|-------------|
| 📊 Métriques système | CPU (%), RAM (utilisée/totale), espace disque |
| 👤 Sessions actives | Utilisateurs connectés, type de session (interactive, RDP, réseau) |
| 🔑 Enrôlement | Auto-enrôlement via token unique au premier démarrage |
| 🔄 Auto-update | Vérification des nouvelles versions via GitHub Releases |

## Architecture

```
[Machine Windows]
    └── synparc-agent (service Windows)
            │
            ├── Collecte métriques (CPU / RAM / Disk)
            ├── Lecture sessions actives (WTS API / logon events)
            │
            └── POST https://<server>/api/v1/agent/heartbeat
                    Authorization: Bearer <enrollment_token>
```

## Configuration

```toml
# synparc-agent.toml
server_url         = "https://synparc.example.com"
enrollment_token   = "xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx"
heartbeat_interval = 60   # secondes
```

## Développement local

```bash
# Go
go build -o synparc-agent .
./synparc-agent --config synparc-agent.toml

# Rust
cargo build --release
./target/release/synparc-agent --config synparc-agent.toml
```

## Déploiement

L'agent est distribué via `synparc-installer` (PowerShell).
Voir → [synparc-installer](https://github.com/Synparc/synparc-installer).
