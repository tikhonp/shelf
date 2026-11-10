// A note says what a name means to Navidrome. Names with nothing worth saying have none.
const fields: Record<string, string> = {
  title: 'The name of this track. It belongs to one track, so select a track to change it.',
  artist: 'Who plays this track. A value with / feat. ft. or ; gets split into several artists unless ARTISTS is set.',
  album: 'Navidrome groups tracks by this name and Album Artist. A track with another spelling leaves the album.',
  albumartist: 'This is the main artist of the album, or Various Artists for a compilation. Every track shares it. Set it even when it matches Artist.',
  track: 'The position on the disc. It belongs to one track, so select a track to change it.',
  disc: 'A multi-disc album stays one album while every disc keeps the same Album.',
  genre: 'One genre per value. Navidrome lists the album under each of them.',
  releasedate: 'The date of this edition, as YYYY, YYYY-MM or YYYY-MM-DD. A plain YEAR counts as the release date.',
  originaldate: 'The first release. While it is set and no release date exists, Navidrome reads DATE as the release date.',
  recordingdate: 'The DATE tag, which is the date of the recording.',
  compilation: 'On for every track of a Various Artists album.',
}

const issues: Record<string, string> = {
  missing_tag: 'Without it Navidrome shows a placeholder or the file name.',
  split_album: 'Navidrome groups by tags, so tracks that differ in Album or Album Artist show as two albums.',
  values_differ: 'These fields should match on every track of an album.',
  tags_disagree: 'Two keys feed one field, and Navidrome reads both.',
  reissue_year: 'No rule can tell which date you want, so this issue has no Fix.',
  no_releasedate: 'RELEASEDATE is this edition. The fix writes it so the year Navidrome shows stays the same.',
  various_artists: 'A compilation needs the flag on every track, or Navidrome files it under the wrong artist.',
  artist_split: 'The IDs name a different number of artists than Navidrome finds in the tags.',
  two_releases: "An album's IDs should all come from one release.",
  no_cover: 'Navidrome shows the folder cover first, then embedded art.',
  bad_cover: "ffmpeg can't decode it. Repair stages a cover and writes nothing.",
}

// A run takes each album alone, with no suggestions, so a note says what it does there.
const fixes: Record<string, string> = {
  no_releasedate: 'Writes RELEASEDATE so the year Navidrome shows stays the same.',
  values_differ: 'Gives every track the value most of them hold. A tie stays as it is.',
  tags_disagree: "Writes the first key's value to every key that feeds the field.",
  various_artists: 'Sets the compilation flag on every track.',
  replaygain: 'Measures each track with one decode, and writes track and album gain to -18 LUFS. It replaces gains already there.',
}

export const fieldNote = (field: string) => fields[field] ?? ''
export const issueNote = (kind: string) => issues[kind] ?? ''
export const fixNote = (fix: string) => fixes[fix] ?? ''
export const scopeNote = 'What an edit reaches. Select a track to edit that track alone.'

// Notes on the Inbox's names and keys. The Accept and Ask for changes notes say what the page used to show at rest.
export const inboxNotes = {
  memory: 'What agents keep between runs: your rules, the preferences they inferred and their notes. Every connection shares it.',
  confidence: "The agent's own rating, from 0 to 100. Shelf doesn't check it or mix it with other suggestions.",
  openAlbum: "Opens the album in the Library, where this proposal's values show as suggestions.",
  revision: 'A revision is a whole new version of the proposal, from the agent or from edits you save. Accept works on the newest.',
  notSure: "What the agent couldn't confirm. Check it before you accept.",
  evidence: 'Where the agent found it. A linked tape opens the source.',
  cable: 'Switching a field on switches on the fields it needs, and switching it off switches off the fields that need it.',
  perTrack: 'Gives each track its own value, so you can change one track alone.',
  tagWrites: 'One tag in one file. A field counts once for each file it writes to.',
  talkback: "Your comments and the agent's replies. The agent reads them on its next run. Shelf doesn't wake it.",
  ask: 'On, the key asks the agent for a new revision. Off, it posts a comment. Neither writes files.',
  declineRest: 'Declines the fields that are off. Shelf writes nothing for them, and the agent reads your reason.',
  declineAll: 'Declines every pending field. Shelf writes nothing, and the agent reads your reason.',
  accept: 'Writes the fields that are on as one change, which History can undo. If the preview changed since you opened it, Shelf shows the new one and asks again.',
}

// reviewNote is the phone's note for a proposal, opened from the ? cap.
export function reviewNote(confidence: number | null | undefined, cabled: boolean): string {
  return [
    'Accept writes the fields that are on, and History can undo it. Decline declines the ones that are off.',
    cabled ? 'The red cable joins fields that switch on and off together.' : '',
    'Press a field to edit its value.',
    confidence != null ? `${confidence}% is the agent's own confidence.` : '',
  ].filter(Boolean).join(' ')
}
