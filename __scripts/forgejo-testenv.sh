#!/usr/bin/env bash
# Disposable Forgejo instance for admin integration tests.
#
#   __scripts/forgejo-testenv.sh start   # start container, create admin, write .env.testenv
#   __scripts/forgejo-testenv.sh stop    # remove container (all data is lost)
#
# Then: set -a; source .env.testenv; set +a; go test -tags integration ./...
#
# The instance listens on 127.0.0.1 only and keeps no volume: stopping it
# discards everything. It may deliver webhooks to any address, so tests can
# receive them on the Docker bridge gateway, and mirror local addresses, so
# tests can mirror one of its repositories to another. Git hooks, which run as
# scripts on the server, can be edited: never allow that on a shared instance.
#
# With GITOPSLIB_TESTENV_RUNNER=1, it also starts an Actions runner in host mode, on
# the instance's network, and writes the label its workflows use. The runner needs
# the internet for nodejs and the actions it fetches; the checks that use it take
# some minutes.
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
	# With a runner, the same port inside as out, so that it can reach the address it
	# is told to use for artifacts. Tests mirroring the instance to itself use 3000.
	local inner=3000
	[ -z "${GITOPSLIB_TESTENV_RUNNER:-}" ] || inner="$PORT"
	docker run -d --name "$NAME" -p "127.0.0.1:${PORT}:${inner}" \
		-e FORGEJO__security__INSTALL_LOCK=true \
		-e FORGEJO__database__DB_TYPE=sqlite3 \
		-e FORGEJO__server__ROOT_URL="${URL}/" \
		-e FORGEJO__server__HTTP_PORT="${inner}" \
		-e FORGEJO__server__DISABLE_SSH=true \
		-e FORGEJO__service__DISABLE_REGISTRATION=true \
		-e FORGEJO__mailer__ENABLED=false \
		-e FORGEJO__webhook__ALLOWED_HOST_LIST='*' \
		-e FORGEJO__webhook__DELIVER_TIMEOUT=5 \
		-e FORGEJO__migrations__ALLOW_LOCALNETWORKS=true \
		-e FORGEJO__security__DISABLE_GIT_HOOKS=false \
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

	# Webhook integration tests listen on the Docker bridge gateway, which
	# the container can reach.
	local hook_host
	hook_host="$(docker network inspect bridge --format '{{(index .IPAM.Config 0).Gateway}}')"

	umask 077
	cat >"$ENV_FILE" <<EOF
GITOPSLIB_TESTENV_URL=$URL
GITOPSLIB_TESTENV_ADMIN_TOKEN=$token
GITOPSLIB_TESTENV_HOOK_HOST=$hook_host
EOF

	if [ -n "${GITOPSLIB_TESTENV_RUNNER:-}" ]; then
		local runner_token
		runner_token="$(docker exec -u git "$NAME" forgejo actions generate-runner-token)"
		docker run -d --name "${NAME}-runner" --network "container:${NAME}" --user 0 --entrypoint sh \
			data.forgejo.org/forgejo/runner:12 -c "apk add --no-cache -q nodejs git >/dev/null 2>&1; cd /tmp && \
				forgejo-runner register --no-interactive --instance http://127.0.0.1:${PORT} --token ${runner_token} --name testenv --labels self:host && \
				forgejo-runner daemon" >/dev/null
		echo "GITOPSLIB_TESTENV_RUNNER_LABEL=self" >>"$ENV_FILE"
		echo "runner started"
	fi
	echo "admin user $ADMIN created; credentials written to $ENV_FILE"
}

stop() {
	docker rm -f "${NAME}-runner" >/dev/null 2>&1 || true
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
