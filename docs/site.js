(() => {
  "use strict";

  const sections = [...document.querySelectorAll(".doc-section[id]")];
  const tocLinks = {};
  for (const a of document.querySelectorAll(".doc-toc__list a[href^='#']")) {
    tocLinks[a.getAttribute("href").slice(1)] = a;
  }

  let activeId = null;
  const setActive = (id) => {
    if (id === activeId) return;
    activeId = id;
    for (const k of Object.keys(tocLinks)) {
      tocLinks[k].classList.toggle("is-active", k === id);
    }
  };

  if (sections.length && "IntersectionObserver" in window) {
    // rootMargin "-15% 0 -70% 0" ties activation to the upper third of the
    // viewport: a section activates once it crosses the 15% line and stays
    // active until it leaves the 30% band.
    const io = new IntersectionObserver(
      (entries) => {
        // DOM order matches visual order — picking by index avoids reading
        // offsetTop and forcing layout.
        let firstVisible = null;
        let firstIdx = Infinity;
        for (const e of entries) {
          if (!e.isIntersecting) continue;
          const idx = sections.indexOf(e.target);
          if (idx < firstIdx) { firstIdx = idx; firstVisible = e.target; }
        }
        if (firstVisible) setActive(firstVisible.id);
      },
      { rootMargin: "-15% 0px -70% 0px", threshold: 0 }
    );
    for (const s of sections) io.observe(s);
    setActive(sections[0].id);
  }

  const faqItems = document.querySelectorAll(".doc-faq__item");
  let openItem = null;
  faqItems.forEach((item, idx) => {
    const btn = item.querySelector(".doc-faq__q");
    if (!btn) return;
    if (idx === 0) { item.classList.add("is-open"); openItem = item; }
    btn.setAttribute("aria-expanded", item.classList.contains("is-open") ? "true" : "false");
    btn.addEventListener("click", () => {
      const wasOpen = item === openItem;
      if (openItem) {
        openItem.classList.remove("is-open");
        openItem.querySelector(".doc-faq__q")?.setAttribute("aria-expanded", "false");
      }
      if (!wasOpen) {
        item.classList.add("is-open");
        btn.setAttribute("aria-expanded", "true");
        openItem = item;
      } else {
        openItem = null;
      }
    });
  });

  const COPY_FEEDBACK_MS = 1400;
  for (const block of document.querySelectorAll(".doc-code")) {
    const btn = block.querySelector(".doc-code__copy");
    const pre = block.querySelector("pre");
    if (!btn || !pre) continue;
    const icon = btn.querySelector("i");
    const label = btn.querySelector(".doc-code__copy-label");
    const barLabel = block.querySelector(".doc-code__label");
    const barText = barLabel?.textContent?.trim() ?? "";
    if (barText) btn.setAttribute("aria-label", `Copy ${barText}`);

    btn.addEventListener("click", async () => {
      // Strip leading "$ " shell prompts so the pasted text is runnable.
      const text = pre.innerText.replace(/^\$\s/gm, "");
      let ok = false;
      try { await navigator.clipboard.writeText(text); ok = true; } catch { /* clipboard blocked */ }
      icon?.classList.replace("ti-copy", ok ? "ti-check" : "ti-x");
      if (label) label.textContent = ok ? "Copied" : "Failed";
      setTimeout(() => {
        icon?.classList.replace(ok ? "ti-check" : "ti-x", "ti-copy");
        if (label) label.textContent = "Copy";
        if (barText) btn.setAttribute("aria-label", `Copy ${barText}`);
      }, COPY_FEEDBACK_MS);
    });
  }
})();
