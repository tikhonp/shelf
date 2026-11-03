# Provenance file

Every download writes `shelf.txt` into its release folder: the album folder, the playlist folder, or the folder a single track landed in. It records where the files came from, so a person browsing the library can tell, and a later phase can link folders to catalog items again without the database.

## When it is written

- The download job's `finish` step writes it, after the source returns its files.
- If the file exists, shelf appends a block. A folder that got a single track and later the whole album keeps both blocks.
- A block whose first line names the same download ID is not written again, so a resumed `finish` step adds no duplicate.
- An artist or label download writes nothing itself. Each of its album downloads writes into its own folder.
- A `partial` download writes its block too, with the failed tracks marked. A `failed` download with no files writes nothing.
- UTF-8 with LF line endings, mode 0644, owned by UID 10001 like the audio files.

## Format

Blocks are separated by one empty line. Each block starts with `Music Shelf download <id>`. The other lines are `Key: value`, then a `Tracks:` list with one track per line, indented by two spaces and split into columns by two spaces.

```
Music Shelf download 418
Shelf link: https://shelf.music.tikhonnnnn.com/downloads/418
Source: Qobuz
Kind: album
Source ID: 123456789
Source link: https://www.qobuz.com/album/example/123456789
Title: Low Tide Hymns
Artist: Northern Static
Label: Example Label
Release date: 2019-03-01
UPC: 0000000000000
Requested format: Qobuz 27, FLAC up to 24-bit 192 kHz
Delivered: FLAC 24-bit 96 kHz
Requested by: Telegram @name, user 123456
Started: 2026-11-03T14:01:52+03:00
Finished: 2026-11-03T14:05:11+03:00
Tools: shelf v0.1.0, qobuz-dl-go v0.1.0 porting qobuz-dl 2.5.9
Tracks:
  1  01 - Shoreline.flac  track 11111111  ISRC XX0000000001  FLAC 24/96  done
  2  02 - Undertow.flac  track 11111112  ISRC XX0000000002  FLAC 24/96  done
```

An Apple Music block:

```
Music Shelf download 419
Shelf link: https://shelf.music.tikhonnnnn.com/downloads/419
Source: Apple Music
Kind: album
Source ID: 1440000000
Source link: https://music.apple.com/ru/album/example/1440000000
Title: Low Tide Hymns
Artist: Northern Static
Label: Example Label
Release date: 2019-03-01
UPC: 0000000000000
Requested format: Apple Music ALAC, converted to FLAC by the amdl config
Delivered: FLAC 24-bit 48 kHz
Requested by: Web
Started: 2026-11-03T15:10:02+03:00
Finished: 2026-11-03T15:12:40+03:00
Tools: shelf v0.1.0, amdl 0.1.4, wrapper-lite storefront ru
Tracks:
  1  01. Shoreline.flac  song 1440000001  ISRC XX0000000001  FLAC 24/48  done
```

## Keys

| Key | Value |
| --- | --- |
| `Shelf link` | `SHELF_PUBLIC_URL/downloads/<id>`. Left out when `SHELF_PUBLIC_URL` is empty. |
| `Source` | `Qobuz` or `Apple Music`. |
| `Kind` | `album`, `playlist` or `track`. |
| `Source ID` | The item's ID in the source's catalog. |
| `Source link` | The item's page on the source's site. |
| `Title`, `Artist`, `Label`, `Release date` | From the catalog. Missing values are left out. |
| `UPC` | For albums, when the catalog has it. |
| `ISRC` | For a single track. |
| `Requested format` | The format asked for, in words. |
| `Delivered` | The highest quality among the files written. |
| `Requested by` | `Web`, or `Telegram @username, user <id>`. |
| `Started`, `Finished` | RFC 3339 in the display time zone, `Europe/Moscow`. |
| `Tools` | shelf's version and the tool that wrote the files. |
| `Tracks` | Position, file name, the track's catalog ID, ISRC, format, and `done`, `skipped` or `failed`. |

Later versions may add keys. Readers ignore keys they don't know and never depend on line order other than the first line.
