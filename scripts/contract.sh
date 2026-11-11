#!/bin/sh
# Runs the Navidrome contract test in Linux containers: a scratch Navidrome
# and the test binary share a Docker volume, so Navidrome sees each write
# as a Linux bind mount would show it. The test's container installs the
# ffmpeg production has, because a run reads that ffmpeg's output.
set -eu
image=${SHELF_CONTRACT_IMAGE:-deluan/navidrome:latest}
name=shelf-contract-$$
work=$(mktemp -d)
cleanup() {
	docker rm -f "$name-nd" >/dev/null 2>&1 || true
	docker network rm "$name" >/dev/null 2>&1 || true
	docker volume rm "$name" >/dev/null 2>&1 || true
	rm -rf "$work"
}
trap cleanup EXIT
arch=$(docker version -f '{{.Server.Arch}}')
GOOS=linux GOARCH=$arch CGO_ENABLED=0 go test -c -tags contract -o "$work/library.test" ./internal/modules/library
docker network create "$name" >/dev/null
docker volume create "$name" >/dev/null
docker run -d --name "$name-nd" --network "$name" -v "$name:/music:ro" \
	-e ND_SCANNER_SCHEDULE=0 -e ND_SCANNER_WATCHERWAIT=0 -e ND_LOGLEVEL=error "$image" >/dev/null
status=0
docker run --rm --network "$name" -v "$name:/music" -v "$work:/t:ro" \
	-v "$PWD/internal/modules/library:/src:ro" -w /src \
	-e SHELF_CONTRACT_NAVIDROME="http://$name-nd:4533" -e SHELF_CONTRACT_MUSIC=/music \
	alpine:3.24 sh -c 'apk add --no-cache -q ffmpeg && exec /t/library.test -test.run TestNavidromeContract -test.v -test.count=1' || status=$?
if [ "$status" -ne 0 ]; then
	docker logs --tail 50 "$name-nd" 2>&1 | tail -50
fi
exit "$status"
