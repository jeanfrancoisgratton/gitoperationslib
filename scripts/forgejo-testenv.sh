#!/usr/bin/env bash
# Disposable Forgejo instance for admin integration tests.
#
#   scripts/forgejo-testenv.sh start   # start container, create admin, write .env.testenv
#   scripts/forgejo-testenv.sh stop    # remove container (all data is lost)
#
# Then: set -a; source .env.testenv; set +a; go test -tags integration ./...
#
# The instance listens on 127.0.0.1 only and keeps no volume: stopping it
# discards everything.
set -euo pipefail

IMAGE="${GITOPSLIB_TESTENV_IMAGE:-codeberg.org/forgejo/forgejo:15}"
NAME="${GITOPSLIB_TESTENV_NAME:-gitopslib-forgejo-test}"
PORT="${GITOPSLIB_TESTENV_PORT:-3999}"
URL="http://127.0.0.1:${PORT}"
ADMIN="gitopslib-admin"
ENV_FILE="$(cd "$(dirname "$0")/.." && pwd)/.env.testenv"

start() {
	if docker container inspect "$NAME" >/dev/null 2>&1; then
		echo "container $NAME already exists; run '$0 stop' first" >&2
		exit 1
	fi
	docker run -d --name "$NAME" -p "127.0.0.1:${PORT}:3000" \
		-e FORGEJO__security__INSTALL_LOCK=true \
		-e FORGEJO__database__DB_TYPE=sqlite3 \
		-e FORGEJO__server__ROOT_URL="${URL}/" \
		-e FORGEJO__server__HTTP_PORT=3000 \
		-e FORGEJO__server__DISABLE_SSH=true \
		-e FORGEJO__service__DISABLE_REGISTRATION=true \
		-e FORGEJO__mailer__ENABLED=false \
		"$IMAGE" >/dev/null

	echo -n "waiting for Forgejo at $URL "
	for _ in $(seq 1 60); do
		if curl -fsS "$URL/api/v1/version" >/dev/null 2>&1; then
			echo " up ($(curl -fsS "$URL/api/v1/version"))"
			break
		fi
		echo -n .
		sleep 1
	done
	curl -fsS "$URL/api/v1/version" >/dev/null || { echo " timed out" >&2; docker logs "$NAME" | tail -20 >&2; exit 1; }

	local password
	password="$(head -c 18 /dev/urandom | base64 | tr -d '/+=')"
	docker exec -u git "$NAME" forgejo admin user create --admin \
		--username "$ADMIN" --password "$password" --email "${ADMIN}@example.invalid" \
		--must-change-password=false >/dev/null
	local token
	token="$(docker exec -u git "$NAME" forgejo admin user generate-access-token \
		--username "$ADMIN" --token-name integration-tests --scopes all --raw)"

	umask 077
	cat >"$ENV_FILE" <<EOF
GITOPSLIB_TESTENV_URL=$URL
GITOPSLIB_TESTENV_ADMIN_TOKEN=$token
EOF
	echo "admin user $ADMIN created; credentials written to $ENV_FILE"
}

stop() {
	docker rm -f "$NAME" >/dev/null 2>&1 && echo "removed $NAME" || echo "$NAME not running"
	rm -f "$ENV_FILE"
}

case "${1:-}" in
start) start ;;
stop) stop ;;
*)
	echo "usage: $0 start|stop" >&2
	exit 2
	;;
esac
