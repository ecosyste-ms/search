import test from "node:test";
import assert from "node:assert/strict";
import {readFileSync} from "node:fs";
import vm from "node:vm";

function panel() {
  const content = {innerHTML: '<a href="/project/example">View packages and repositories</a>'};
  const item = {open: false, dataset: {url: "/project/example?fragment=identities"},
    addEventListener(type, fn) { assert.equal(type, "toggle"); this.toggle = fn; },
    querySelector(selector) { assert.equal(selector, ".identity-list"); return content; }};
  const requests = [];
  vm.runInNewContext(readFileSync(new URL("./assets/search.js", import.meta.url), "utf8"), {
    document: {querySelectorAll(selector) { assert.equal(selector, "details[data-url]"); return [item]; }},
    addEventListener() {},
    fetch(url) { return new Promise(resolve => requests.push({url, resolve})); },
  });
  return {item, content, requests};
}

test("only expanding public identities makes a request, and only once", async () => {
  const {item, content, requests} = panel();
  assert.equal(requests.length, 0);
  await item.toggle();
  assert.equal(requests.length, 0);
  item.open = true;
  const pending = item.toggle();
  await item.toggle();
  assert.equal(requests.length, 1);
  assert.equal(requests[0].url, "/project/example?fragment=identities");
  requests[0].resolve({ok: true, text: async () => "<p>pkg:nix/curl?channel=unstable</p>"});
  await pending;
  assert.match(content.innerHTML, /pkg:nix/);
  item.open = false; await item.toggle();
  item.open = true; await item.toggle();
  assert.equal(requests.length, 1);
});

test("Back restores the submitted query instead of an unsent form edit", () => {
  const input = {defaultValue: "curl", value: "numpy"};
  let restore;
  vm.runInNewContext(readFileSync(new URL("./assets/search.js", import.meta.url), "utf8"), {
    document: {querySelectorAll() {return [];}, getElementById(id) {assert.equal(id, "query"); return input;}},
    addEventListener(name, fn) {assert.equal(name, "pageshow"); restore = fn;},
  });
  restore();
  assert.equal(input.value, "curl");
});

test("failed expansion preserves the normal HTML link and can be retried", async () => {
  const {item, content, requests} = panel();
  item.open = true;
  const pending = item.toggle();
  requests[0].resolve({ok: false});
  await pending;
  assert.ok(content.innerHTML.includes('href="/project/example"'));
  assert.equal(item.dataset.loaded, undefined);
  const retry = item.toggle();
  requests[1].resolve({ok: true, text: async () => "<p>Package identities</p>"});
  await retry;
  assert.equal(content.innerHTML, "<p>Package identities</p>");
});
