"use strict";

addEventListener("pageshow", () => {
  const input = document.getElementById("query");
  if (input) input.value = input.defaultValue;
});

for (const panel of document.querySelectorAll("details[data-url]")) {
  panel.addEventListener("toggle", async () => {
    if (!panel.open || panel.dataset.loaded) return;
    panel.dataset.loaded = "true";
    try {
      const response = await fetch(panel.dataset.url);
      if (!response.ok) throw new Error("Details unavailable");
      panel.querySelector(".identity-list").innerHTML = await response.text();
    } catch {
      delete panel.dataset.loaded;
    }
  });
}
