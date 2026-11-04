#!/bin/sh
# Stands in for amdl in the runner's tests. It prints the lines amdl's
# rip.go prints and writes nothing.
#
#   FAKE_AMDL   "ok" (the default) lists two of three songs, "fail" exits 1
#               after a failed song, "hang" never ends, "stubborn" never ends
#               and ignores SIGTERM, "env" prints its folder, environment
#               and arguments, "early" stops before its summary, "replay"
#               prints the recorded run in FAKE_REPLAY
#   FAKE_MUSIC  the folder the listed paths are under
#   FAKE_PIDS   a file that gets the PID of the child "hang" starts

case "${FAKE_AMDL:-ok}" in
  replay)
    cat "$FAKE_REPLAY"
    exit 0
    ;;
  env)
    echo "pwd=$(pwd)"
    echo "cache=$XDG_CACHE_HOME"
    echo "tmp=$TMPDIR"
    echo "args=$*"
    echo "[]"
    exit 0
    ;;
  early)
    echo "lite-server regions: []"
    echo "Failed to get token."
    exit 0
    ;;
  hang|stubborn)
    if [ "$FAKE_AMDL" = stubborn ]; then trap '' TERM; fi
    sleep 600 &
    echo "$!" > "$FAKE_PIDS"
    echo "Album"
    while :; do sleep 0.1; done
    ;;
esac

echo "lite-server regions: [ru]"
echo "Queue 1 of 1: Album"
echo "Northern Static - Low Tide Hymns (2019)[24B-96kHz]"
echo "Track 1 of 3: songs"
echo "01. Shoreline"
printf 'Decrypting...  10%%  (1.0/9.9 MB, 5.0 MB/s) \rDecrypting...  60%%  (5.9/9.9 MB, 5.0 MB/s) \rDecrypting... 100%%  (9.9/9.9 MB, 5.0 MB/s) [2s] Decrypted\n'
echo "Converting -> flac ..."
echo "Conversion completed in 1.2s: 01. Shoreline.flac"
echo "Original removed."
echo "Track 2 of 3: songs"
echo "02. Undertow"
echo "Converted track already exists locally."
echo "Track 3 of 3: songs"
if [ "${FAKE_AMDL:-ok}" = fail ]; then
  echo "03. Breakwater"
  echo "Failed to run v4: key retrieval failed"
  echo "=======  [✔ ] Completed: 2/3  |  [⚠ ] Warnings: 0  |  [✖ ] Errors: 1  ======="
  echo "Error detected, exiting..."
  exit 1
fi
echo "Unavailable"
echo "=======  [✔ ] Completed: 2/3  |  [⚠ ] Warnings: 1  |  [✖ ] Errors: 0  ======="
d="$FAKE_MUSIC/apple-music/Northern Static - Low Tide Hymns (2019)[24B-96kHz]"
echo "[{\"path\":\"$d/01. Shoreline.flac\",\"artist\":\"Northern Static\",\"artist_id\":\"111\",\"album\":\"Low Tide Hymns\",\"song\":\"Shoreline\"},{\"path\":\"$d/02. Undertow.flac\",\"artist\":\"Northern Static\",\"artist_id\":\"111\",\"album\":\"Low Tide Hymns\",\"song\":\"Undertow\"}]"
