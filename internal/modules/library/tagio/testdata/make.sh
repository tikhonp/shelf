#!/bin/sh
# Writes the untagged fixtures, half a second of a sine each. The tests add
# tags and pictures to copies, so the files stay small and carry no one's
# music. Rerun only to change a fixture, and commit the output.
set -eu
cd "$(dirname "$0")"
sine() { r=$1; shift; ffmpeg -v error -y -f lavfi -i "sine=frequency=440:duration=0.5:sample_rate=$r" -ac 2 -map_metadata -1 -fflags +bitexact -flags:a +bitexact "$@"; }
sine 44100 -c:a flac -sample_fmt s16 track.flac
sine 44100 -c:a libmp3lame -b:a 64k track.mp3
sine 44100 -c:a aac -b:a 64k track.m4a
sine 44100 -c:a alac alac.m4a
sine 44100 -c:a vorbis -strict -2 track.ogg
sine 48000 -c:a libopus -b:a 32k track.opus
sine 44100 -c:a pcm_s16le -t 0.2 track.wav
sine 44100 -c:a pcm_s16be -t 0.2 track.aiff
sine 44100 -c:a wmav2 -b:a 64k track.wma
