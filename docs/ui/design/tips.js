// Review-only copy of LibTip: a note opens 350ms after the pointer reaches a name and closes when it leaves.
// A page opened with #note shows its pinned note, the way the Library's note board does.
// The phone has no hover: a ? cap in the title bar opens the page's note in a slot above the dock.
(() => {
  const root = document.querySelector(".rs");
  let timer, box, owner;
  function hide() {
    clearTimeout(timer);
    if (box) box.remove();
    if (owner) owner.removeAttribute("aria-describedby");
    box = owner = null;
  }
  function show(el) {
    hide();
    const r = el.getBoundingClientRect(), R = root.getBoundingClientRect();
    box = document.createElement("span");
    box.className = "tipbox"; box.id = "tip"; box.setAttribute("role", "tooltip");
    const h = document.createElement("span"); h.className = "tip-h"; h.textContent = el.dataset.tipH;
    const t = document.createElement("span"); t.textContent = el.dataset.tip;
    box.append(h, t);
    root.appendChild(box);
    const x = Math.max(8, Math.min(r.left - R.left, R.width - box.offsetWidth - 8));
    let y = r.bottom - R.top + 6;
    if (y + box.offsetHeight > R.height - 8) y = r.top - R.top - box.offsetHeight - 6;
    box.style.left = x + "px"; box.style.top = y + "px";
    owner = el; el.setAttribute("aria-describedby", "tip");
  }
  document.querySelectorAll("[data-tip]").forEach((el) => {
    el.addEventListener("mouseenter", () => { clearTimeout(timer); timer = setTimeout(() => show(el), 350); });
    el.addEventListener("mouseleave", hide);
  });
  const pinned = document.querySelector("[data-tip-pinned]");
  if (pinned && location.hash === "#note") show(pinned);

  const q = document.querySelector(".wbtn.q"), slot = document.getElementById("note-slot");
  if (q && slot) {
    const set = (on) => { slot.hidden = !on; q.setAttribute("aria-pressed", String(on)); };
    q.addEventListener("click", () => set(slot.hidden));
    slot.querySelector("[data-close]").addEventListener("click", () => set(false));
    set(location.hash === "#note");
  }
})();
