#!/usr/bin/env bash
set -euo pipefail

if [[ "${GITHUB_ACTIONS:-}" != true || "${MYSQL_DATABASE:-}" != phantomframe || "${PHANTOMFRAME_IMAGE:-}" != phantomframe:ci ]]; then
  echo 'Run only in the isolated GitHub Actions deployment test job.' >&2
  exit 1
fi

domain=phantomframe.tcmzhan.com
test_dir=$(mktemp -d)
trap 'docker rm -f phantomframe-nginx-check >/dev/null 2>&1 || true' EXIT

compose() {
  docker compose --env-file deploy/.env -f deploy/docker-compose.yml "$@"
}
https() {
  curl --silent --show-error --insecure --noproxy "$domain" \
    --connect-timeout 5 --max-time 20 \
    --resolve "$domain:443:127.0.0.1" "$@"
}
wait_for_api() {
  for attempt in $(seq 1 60); do
    if curl --fail --silent http://127.0.0.1:3001/api/health >/dev/null; then
      return 0
    fi
    sleep 2
  done
  docker logs phantomframe
  return 1
}

cp deploy/.env.example deploy/.env
compose config --quiet
compose up -d --pull never
wait_for_api
curl --fail --silent http://127.0.0.1:3001/api/health | jq -e '.status == "ok" and .database == "mysql" and .cache == "redis"'
docker exec phantomframe sh -c 'test -f /app/dist/index.html && command -v ffprobe && ! command -v nginx && test ! -f /app/.env && test "$(id -u)" -ne 0'

# Nginx is a separate test process, never part of the application image.
openssl req -x509 -newkey rsa:2048 -nodes -days 1 \
  -subj "/CN=$domain" \
  -keyout "$test_dir/privkey.pem" -out "$test_dir/fullchain.pem" 2>/dev/null
docker run --detach --name phantomframe-nginx-check --network host \
  --mount type=bind,source="$PWD/deploy/nginx/phantomframe.tcmzhan.com.conf",target=/etc/nginx/conf.d/default.conf,readonly \
  --mount type=bind,source="$test_dir",target="/etc/letsencrypt/live/$domain",readonly \
  nginx:stable-alpine
docker exec phantomframe-nginx-check nginx -t
for attempt in $(seq 1 30); do
  if https --fail --output /dev/null "https://$domain/api/health" 2>/dev/null; then
    break
  fi
  sleep 1
done
https --fail "https://$domain/api/health" | jq -e '.status == "ok"'
redirect=$(curl --silent --noproxy "$domain" --resolve "$domain:80:127.0.0.1" \
  --output /dev/null --write-out '%{http_code}|%{redirect_url}' "http://$domain/generate")
test "$redirect" = "301|https://$domain/generate"

# Verify React deep links and the hashed JavaScript/CSS files behind HTTPS.
https --fail "https://$domain/generate" > "$test_dir/index.html"
grep -q '<div id="root">' "$test_dir/index.html"
grep -oE '/assets/[^" ]+\.(js|css)' "$test_dir/index.html" > "$test_dir/assets.txt"
while IFS= read -r asset; do
  https --fail --output /dev/null "https://$domain$asset"
done < "$test_dir/assets.txt"

https --fail -c "$test_dir/cookies" -D "$test_dir/login.headers" \
  -H 'Content-Type: application/json' -H "Origin: https://$domain" \
  --data '{"username":"smoke-admin","password":"phantomframe-ci-admin-only"}' \
  "https://$domain/api/auth/login" | jq -e '.username == "smoke-admin"'
grep -i 'set-cookie:' "$test_dir/login.headers" | grep -qi 'secure'
https --fail -b "$test_dir/cookies" "https://$domain/api/me" | jq -e '.username == "smoke-admin"'
status=$(https --output /dev/null --write-out '%{http_code}' \
  -H 'Origin: https://unrelated.example' -H 'Content-Type: application/json' \
  --data '{}' "https://$domain/api/auth/login")
test "$status" = 403

api_key=$(https --fail -b "$test_dir/cookies" -H "Origin: https://$domain" \
  -H 'Content-Type: application/json' --data '{"name":"deployment-check"}' \
  "https://$domain/api/keys" | jq -er '.token')
https --fail -H "Authorization: Bearer $api_key" "https://$domain/api/me" | jq -e '.username == "smoke-admin"'

# An actual uploaded file and Redis session must survive container replacement.
printf '%s' 'iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAusB9Y9Zl1sAAAAASUVORK5CYII=' | base64 --decode > "$test_dir/reference.png"
asset_url=$(https --fail -b "$test_dir/cookies" -H "Origin: https://$domain" \
  -F "file=@$test_dir/reference.png;type=image/png" "https://$domain/api/assets" | jq -er '.url')
compose down
compose up -d --pull never
wait_for_api
https --fail -b "$test_dir/cookies" "https://$domain/api/me" | jq -e '.username == "smoke-admin"'
https --fail -b "$test_dir/cookies" "https://$domain$asset_url" > "$test_dir/restored.png"
cmp "$test_dir/reference.png" "$test_dir/restored.png"
echo 'Verified HTTPS proxy, redirect, frontend assets, secure login, API key, origin checks, file persistence and session persistence across container replacement.'
