<template>
  <div class="lib-guide">
    <div>
      <h3 class="px t3">How Navidrome sees an album</h3>
      <ul><li>Navidrome groups music by tags alone, and ignores folder and file names.</li><li>An album holds together when every track has the same Album and Album Artist.</li><li>A multi-disc album stays one album while every disc keeps the same Album.</li></ul>
      <h3 class="px t3">Working through an album</h3>
      <ol><li>Press Fix on each issue. A Fix stages edits and writes nothing.</li><li>Set Album Artist first, on All tracks.</li><li>Take suggestions one by one. Release fields all come from one release, so their IDs can't mix.</li><li>Read the amber fields, then press Save. One Save is one undo step.</li><li>After the scan, check the album in Navidrome under Get Info, Raw Tags.</li></ol>
    </div>
    <div>
      <h3 class="px t3">Tags every track needs</h3>
      <ul><li>Every track needs Title, Artist, Album, Album Artist and Track. Genre, dates and Disc are optional.</li><li>Set Album Artist even when it matches Artist.</li><li>Replace blanks and values such as Unknown Artist with real ones.</li><li>Keep spelling the same everywhere. A trailing space or different capitals make a duplicate.</li></ul>
      <h3 class="px t3">Several artists</h3>
      <ul><li>ARTISTS and ALBUMARTISTS hold one artist per value, and Navidrome never splits them. They are the preferred way.</li><li>ARTIST then stays the display name, such as Alice feat. Bob.</li><li>ARTIST alone gets split on / feat. ft. and ; with spaces around them. AC/DC stays whole, and Foo; Bar breaks.</li></ul>
      <h3 class="px t3">Compilations</h3>
      <ul><li>Album Artist is Various Artists, and the compilation flag is on for every track.</li></ul>
    </div>
    <div>
      <h3 class="px t3">Dates</h3>
      <ul><li>A date is YYYY, YYYY-MM or YYYY-MM-DD.</li><li>RELEASEDATE is this edition, ORIGINALDATE is the first release, and DATE is the recording date. A plain YEAR counts as the release date.</li><li>While ORIGINALDATE is set and no release date exists, Navidrome reads DATE as the release date.</li></ul>
      <h3 class="px t3">Covers</h3>
      <ul><li>A cover of 500x500 to 1000x1000 is plenty. shelf stops embedded art at 1000x1000 and keeps the folder file at full size.</li><li>Navidrome shows the folder cover first, so a cover edit changes both.</li></ul>
    </div>
  </div>
  <p class="t2 sm source">Based on <a href="https://www.navidrome.org/docs/usage/library/tagging/" target="_blank" rel="noopener">Navidrome's tagging guidelines</a>.</p>
</template>

<style scoped>
.rs h3 { padding: 10px 0 6px; font-size: 12px; }
.rs .source { padding: 0 18px 16px; }
.rs .source a { color: var(--sun-text); }
</style>
