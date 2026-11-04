#!/bin/sh
# Stands in for /app/wrapper-lite-rootless in shelf-wrapper's tests. It
# prints the lines wrapper-lite prints and reads the code file as it does.
#
#   FAKE_DATA     the folder that --base-dir names, as the supervisor sees it
#   FAKE_SERVICE  service mode: "run" waits for SIGINT (the default), "exit"
#                 exits at once, "stubborn" ignores SIGINT and starts a
#                 child that only a SIGKILL to the group ends
#   FAKE_LOGIN    sign-in: "code" asks for a code (the default), "direct"
#                 signs in without one, "fail" fails at once
#   FAKE_CODE     the code that signs in, 123456 by default
#   FAKE_WAIT     tenths of a second to wait for the code, 600 by default
#   FAKE_ECHO     when set, the arguments are printed, as wrapper-lite does
#                 for an unknown flag
#
# Service mode creates $FAKE_DATA/listening, and the tests' /status answers
# only while it exists.

# Levels are padded to five letters, as lite/logger.h pads them.
say() { printf '%s.000 [%-5s] %s\n' "$(date -u '+%Y-%m-%d %H:%M:%S')" "$1" "$2"; }

mode=service
prev=""
for arg in "$@"; do
  if [ "$prev" = "--login" ]; then mode=login; fi
  prev=$arg
done
if [ -n "$FAKE_ECHO" ]; then say ERROR "unknown argument: $*"; fi

if [ "$mode" = login ]; then
  # The real launcher kills wrapper-lite on SIGINT and exits.
  trap 'exit 0' INT
  say INFO "wrapper-lite login mode"
  say INFO "logging in..."
  case "${FAKE_LOGIN:-code}" in
    fail)
      say WARN "auth error: code=-20101, message=Your Apple ID or password was entered incorrectly."
      say ERROR "login failed"
      exit 0
      ;;
    server)
      say WARN "server message: Your Apple ID or password was incorrect."
      say WARN "auth error: code=2076505080, message=iTunesStoreErrorCategory"
      say ERROR "login failed"
      exit 0
      ;;
    code)
      say INFO "credentialHandler: {title: Sign In, message: , 2FA: true}"
      say WARN "Enter your 2FA code into /data/2fa.txt"
      n=0
      while [ ! -f "$FAKE_DATA/2fa.txt" ] && [ "$n" -lt "${FAKE_WAIT:-600}" ]; do
        sleep 0.1
        n=$((n + 1))
      done
      if [ ! -f "$FAKE_DATA/2fa.txt" ]; then
        say WARN "2FA code timeout (60s), aborting login"
        say ERROR "failed to cache account info"
        exit 0
      fi
      code=$(cat "$FAKE_DATA/2fa.txt")
      rm -f "$FAKE_DATA/2fa.txt"
      say WARN "Code file detected! Logging in..."
      if [ "$code" != "${FAKE_CODE:-123456}" ]; then
        say WARN "auth error: code=-22421, message=Incorrect verification code."
        say ERROR "login failed"
        exit 0
      fi
      ;;
  esac
  say INFO "login successful"
  echo ru > "$FAKE_DATA/STOREFRONT_ID"
  say INFO "login complete, exiting"
  exit 0
fi

case "${FAKE_SERVICE:-run}" in
  exit)
    say ERROR "fake crash in service mode"
    exit 3
    ;;
  stubborn)
    trap '' INT
    sleep 300 &
    ;;
  *)
    trap 'rm -f "$FAKE_DATA/listening"; say INFO "received signal 2, stopping service"; say INFO "wrapper-lite stopped"; exit 0' INT
    ;;
esac
# The tests' /status answers while this file exists.
touch "$FAKE_DATA/listening"
say INFO "wrapper-lite listening on 0.0.0.0:12340"
say INFO "request: GET /status"
while :; do
  sleep 0.1
done
