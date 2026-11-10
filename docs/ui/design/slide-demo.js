// Review-only demo of the passkey slide: OFF to ON asks for the passkey, a second press cancels it.
// The slide also answers a drag, and pressing the key switch beside it does the same as the slide.
// The key switch picture turns to ON when the passkey signs in, and back to OFF with the slide.
document.querySelectorAll(".slide[data-demo]").forEach((sw) => {
  const unit = sw.closest("section");
  const lcd = unit.querySelector(".lcd[role=status]");
  const bar = document.querySelectorAll("[data-power] .led");
  const pic = unit.querySelector(".keysw");
  const bow = pic && pic.querySelector("g");
  if (bow) {
    bow.removeAttribute("transform");
    bow.style.transformOrigin = "75px 72px";
    bow.style.transition = matchMedia("(prefers-reduced-motion: reduce)").matches ? "none" : "transform .5s cubic-bezier(.3, 1.5, .5, 1)";
  }
  const turn = (on) => { if (bow) bow.style.transform = `rotate(${on ? 40 : -40}deg)`; };
  turn(false);
  let timers = [];
  const show = (a, b, kind) => {
    lcd.className = "lcd" + (kind ? " " + kind : "");
    lcd.innerHTML = `<div class="lcd-l dim">MUSIC SHELF</div><div class="lcd-l">${a}</div><div class="lcd-l">${b}</div>`;
  };
  const clear = () => { timers.forEach(clearTimeout); timers = []; };
  let state = "out";
  function toggle() {
    clear();
    if (state === "out") {
      state = "waiting"; sw.setAttribute("aria-checked", "true");
      show("WAITING FOR", "YOUR PASSKEY");
      timers.push(setTimeout(() => {
        state = "hello"; turn(true); show("HELLO, TIKHON", "OPENING THE LIBRARY");
        bar.forEach((l, i) => timers.push(setTimeout(() => { l.className = "led ok"; }, 260 * (i + 1))));
      }, 1800));
    } else {
      const was = state; state = "out"; sw.setAttribute("aria-checked", "false"); turn(false);
      bar.forEach((l) => { l.className = "led off"; });
      if (was === "waiting") show("PASSKEY CANCELED", "USE THE PASSWORD", "warn"); else show("SIGNED OUT", "PASSWORD OR PASSKEY");
    }
  }
  // A drag of 24px toward the other end flips it, so a swipe works as well as a press.
  let from = null, dragged = false;
  sw.addEventListener("pointerdown", (e) => { from = e.clientX; dragged = false; });
  sw.addEventListener("pointermove", (e) => {
    if (from === null) return;
    const d = e.clientX - from, on = sw.getAttribute("aria-checked") === "true";
    if ((!on && d > 24) || (on && d < -24)) { dragged = true; from = null; toggle(); }
  });
  sw.addEventListener("pointerup", () => { from = null; });
  sw.addEventListener("click", () => { if (dragged) { dragged = false; return; } toggle(); });
  if (pic && pic.hasAttribute("data-press")) pic.addEventListener("click", toggle);
});
