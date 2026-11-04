<template>
  <div class="col g12 pad" aria-busy="true">
    <div class="lcd loading"><span class="led run blink"></span><span class="lcd-l">READING FILES</span></div>
    <span class="sr">Loading the album</span>
    <div class="line g12"><span class="sk art"></span><span class="col g8 grow"><span class="sk wide"></span><span class="sk mid"></span></span></div>
    <span class="sk tall"></span><span class="sk tall"></span><span class="sk tall"></span>
  </div>
</template>

<style scoped>
.rs .pad { padding: 12px; }
.rs .loading { flex: none; flex-direction: row; align-items: center; justify-content: flex-start; gap: 10px; }
.rs .art { width: 72px; height: 72px; }
.rs .wide { width: 80%; }
.rs .mid { width: 55%; }
.rs .tall { height: 36px; }
</style>
