# API GO - Phase 1 MVP

API REST en Go pour decrire un projet multi-conteneurs et le deployer via Docker Engine local.

**Repository:** https://github.com/ravenelthomas/API-GO

## Fonctionnalites MVP

- Description de projets (`projects`) et services (`services`)
- Deploiement d'un projet (`deployments`) vers un serveur cible
- Gestion des images (pull implicite lors du deploiement)
- Gestion des volumes nommes
- Variables d'environnement par deploiement
- Multi-engine via gestion de `servers`
- Scaling via `replicas` par service et scale bulk
- Support Phase 3 initial: networks, secrets, labels
- Etat global d'un deploiement (`running`, `not-running`, `partially-running`)
- Authentification JWT pour securiser l'API

## Lancer en local

```bash
go run ./cmd/api
```

Variables utiles:

- `PORT` (defaut: `8080`)
- `DB_PATH` (defaut: `data/api.db`)
- `JWT_SECRET` (defaut: `default-secret-key-change-in-production`)

## Build Docker

```bash
docker build -t api-go-mvp .
docker run --rm -p 8080:8080 -v $(pwd)/data:/app/data -v /var/run/docker.sock:/var/run/docker.sock api-go-mvp
```

## Lancer en dev avec Docker Compose

```bash
docker compose up --build
```

## Endpoints

**Public routes:**
- `GET /health`
- `POST /auth/register`
- `POST /auth/login`

**Protected routes (require JWT token):**
- `POST /servers`
- `GET /servers`
- `GET /servers/:id`
- `POST /images/pull`
- `POST /images/build`
- `POST /projects`
- `GET /projects`
- `GET /projects/:id`
- `POST /projects/:id/services`
- `POST /projects/:id/networks`
- `POST /projects/:id/secrets`
- `POST /projects/:id/labels`
- `POST /projects/:id/deployments`
- `GET /deployments`
- `GET /deployments/:id`
- `GET /deployments/:id/status`
- `POST /deployments/:id/scale`

## Exemples rapides

**S'inscrire:**

```bash
curl -X POST localhost:8080/auth/register \
  -H "Content-Type: application/json" \
  -d '{"username":"admin","password":"securepassword"}'
```

**Se connecter:**

```bash
curl -X POST localhost:8080/auth/login \
  -H "Content-Type: application/json" \
  -d '{"username":"admin","password":"securepassword"}'
```

La réponse contiendra un token JWT à utiliser dans les requêtes protégées:

```json
{
  "token": "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9...",
  "user_id": 1
}
```

**Utiliser les endpoints protégés:**

Ajoutez le header `Authorization: Bearer <token>` à toutes les requêtes protégées.

Creer un projet:

```bash
curl -X POST localhost:8080/projects \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer <token>" \
  -d '{"name":"wordpress","description":"stack wp"}'
```

Ajouter un service:

```bash
curl -X POST localhost:8080/projects/1/services \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer <token>" \
  -d '{"name":"db","image":"mysql:8","volume_name":"mysql-data","volume_path":"/var/lib/mysql"}'
```

Deployer:

```bash
curl -X POST localhost:8080/projects/1/deployments \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer <token>" \
  -d '{
    "name":"wp-dev",
    "server_id":1,
    "env_overrides":{"db":{"MYSQL_ROOT_PASSWORD":"rootpwd"}},
    "port_mappings":{"db":{"3306/tcp":"3306"}},
    "replicas":{"db":2}
  }'
```

Pull d'image:

```bash
curl -X POST localhost:8080/images/pull \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer <token>" \
  -d '{"image":"nginx:alpine","server_id":1}'
```

Build d'image locale:

```bash
curl -X POST localhost:8080/images/build \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer <token>" \
  -d '{"context_dir":".","tag":"api-go-local:test","server_id":1}'
```

Scale d'un service:

```bash
curl -X POST localhost:8080/deployments/2/scale \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer <token>" \
  -d '{"service":"web","replicas":3}'
```

Scale bulk:

```bash
curl -X POST localhost:8080/deployments/2/scale \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer <token>" \
  -d '{"replicas_by_service":{"web":2,"worker":1}}'
```

Ajouter un reseau projet (avec subnet/gateway optionnels):

```bash
curl -X POST localhost:8080/projects/1/networks \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer <token>" \
  -d '{"name":"frontend","driver":"bridge","subnet":"172.30.0.0/24","gateway":"172.30.0.1"}'
```

Ajouter un secret projet (injecte au deploiement, et tentative secret Docker natif si support moteur):

```bash
curl -X POST localhost:8080/projects/1/secrets \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer <token>" \
  -d '{"name":"db_password"}'
```

Ajouter un label projet:

```bash
curl -X POST localhost:8080/projects/1/labels \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer <token>" \
  -d '{"key":"traefik.enable","value":"true"}'
```
