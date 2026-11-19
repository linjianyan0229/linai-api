#!/usr/bin/env bash
set -euo pipefail

action="${1:-up}"
server_port=3001
project_name="linai-api-remote-test"
compose_files=(-f docker-compose.dev.yml -f docker-compose.remote-test.yml)
deploy_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
env_file="${HOME}/.config/linai-api-docker-test.env"

cd "$deploy_dir"

if [[ "$action" == "down" ]]; then
  if [[ ! -f "$env_file" ]]; then
    echo "No remote Docker test environment has been created."
    exit 0
  fi
  instabox-port unpublish "$server_port" >/dev/null 2>&1 || true
  docker compose --project-name "$project_name" --env-file "$env_file" "${compose_files[@]}" down
  exit 0
fi

if [[ "$action" != "up" ]]; then
  echo "Usage: $0 [up | down]" >&2
  exit 2
fi

mkdir -p "$(dirname "$env_file")"
if [[ ! -f "$env_file" ]]; then
  umask 077
  cat > "$env_file" <<EOF
BIND_HOST=0.0.0.0
SERVER_PORT=${server_port}
POSTGRES_PASSWORD=$(openssl rand -hex 32)
REDIS_PASSWORD=$(openssl rand -hex 32)
JWT_SECRET=$(openssl rand -hex 32)
TOTP_ENCRYPTION_KEY=$(openssl rand -hex 32)
SUB2API_DEV_HTTP_PROXY=
SUB2API_DEV_HTTPS_PROXY=
SUB2API_DEV_ALL_PROXY=
SUB2API_DEV_NO_PROXY=127.0.0.1,localhost,::1,postgres,redis,sub2api
EOF
fi
if ! grep -q '^ADMIN_EMAIL=.' "$env_file"; then
  sed -i '/^ADMIN_EMAIL=/d' "$env_file"
  printf 'ADMIN_EMAIL=admin@sub2api.local\n' >> "$env_file"
fi
if ! grep -q '^ADMIN_PASSWORD=.' "$env_file"; then
  sed -i '/^ADMIN_PASSWORD=/d' "$env_file"
  printf 'ADMIN_PASSWORD=%s\n' "$(openssl rand -hex 24)" >> "$env_file"
fi
chmod 600 "$env_file"

docker compose --project-name "$project_name" --env-file "$env_file" "${compose_files[@]}" config --quiet
docker compose --project-name "$project_name" --env-file "$env_file" "${compose_files[@]}" up -d --build

ready=false
for _ in $(seq 1 180); do
  if curl -fsS --max-time 2 "http://127.0.0.1:${server_port}/health" >/dev/null 2>&1; then
    ready=true
    break
  fi
  sleep 2
done

if [[ "$ready" != true ]]; then
  docker compose --project-name "$project_name" --env-file "$env_file" "${compose_files[@]}" ps
  echo "The service did not become healthy on port ${server_port}." >&2
  exit 1
fi

docker compose --project-name "$project_name" --env-file "$env_file" "${compose_files[@]}" ps
preview_url="$(instabox-port publish "$server_port" --name linai-api-test | sed -n '1p')"
if [[ "$preview_url" != https://* ]]; then
  echo "Could not publish the web preview port." >&2
  exit 1
fi
printf 'HEALTHCHECK=passed\n'
printf 'PREVIEW_URL=%s\n' "$preview_url"
printf 'LOGIN_EMAIL=%s\n' "$(sed -n 's/^ADMIN_EMAIL=//p' "$env_file" | tail -n 1)"
printf 'LOGIN_PASSWORD=%s\n' "$(sed -n 's/^ADMIN_PASSWORD=//p' "$env_file" | tail -n 1)"
