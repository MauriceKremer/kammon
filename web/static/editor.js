// The script editor on the Automations page: the unsaved-changes note, the leave confirmation
// (MON-AU-14), the long-timeout note (MON-AU-13) and the console lines for actions the server
// could not answer, which htmx does not swap by itself. Everything delegates through document
// events, so the out-of-band swaps of the page leave the behaviour in place.
(function () {
  var TIMEOUT_NOTE_AFTER = 5000; // ms, MON-AU-13

  function editor() {
    return document.getElementById("script-editor");
  }

  function setDirty(dirty) {
    var form = editor();
    if (!form) return;
    if (dirty) {
      form.setAttribute("data-dirty", "true");
    } else {
      form.removeAttribute("data-dirty");
    }
    var note = document.getElementById("unsaved-note");
    if (note) note.hidden = !dirty;
  }

  function isDirty() {
    var form = editor();
    return !!form && form.getAttribute("data-dirty") === "true";
  }

  // Typing in the editor's own fields marks the page dirty; the enabled control and the recover
  // field ask KAM directly and never count (MON-AU-10, MON-AU-11).
  document.addEventListener("input", function (evt) {
    var form = editor();
    if (!form || !evt.target || !form.contains(evt.target)) return;
    if (evt.target.id === "script-enabled-box" || evt.target.id === "recover-timeout") return;
    setDirty(true);
  });

  window.addEventListener("beforeunload", function (evt) {
    if (!isDirty()) return;
    evt.preventDefault();
    evt.returnValue = ""; // the browser's own words; the requirement is the ask (MON-AU-14)
  });

  // A timeout above 5000 ms blocks the other scripts: KAM runs one at a time (MON-AU-13).
  function timeoutNote() {
    var field = document.getElementById("script-timeout");
    var note = document.getElementById("timeout-note");
    if (!field || !note) return;
    var value = parseInt(field.value, 10);
    note.hidden = !(value > TIMEOUT_NOTE_AFTER);
  }
  document.addEventListener("input", function (evt) {
    if (evt.target && evt.target.id === "script-timeout") timeoutNote();
  });
  document.addEventListener("DOMContentLoaded", timeoutNote);
  document.addEventListener("htmx:afterSwap", timeoutNote);

  // A saved answer clears the note; an unanswered one does not (nothing was saved). htmx fires
  // afterSwap on the element it just replaced, which may already be detached, so look up the
  // live console in the document and consume the flag, so a later swap cannot re-clear it.
  document.addEventListener("htmx:afterSwap", function () {
    var consoleBox = document.getElementById("console");
    if (!consoleBox || consoleBox.getAttribute("data-saved") !== "true") return;
    consoleBox.removeAttribute("data-saved");
    setDirty(false);
  });

  // The server's answers always swap; these statuses are the ones that never reached it: the
  // guard refused the request, or KAMmon did not get through. Say so, keep the editor untouched.
  function writeFail(text) {
    var consoleBox = document.getElementById("console");
    if (!consoleBox) return;
    consoleBox.removeAttribute("data-saved");
    while (consoleBox.firstChild) consoleBox.removeChild(consoleBox.firstChild);
    var line = document.createElement("p");
    line.className = "fail";
    line.textContent = text;
    consoleBox.appendChild(line);
  }

  document.body.addEventListener("htmx:responseError", function (evt) {
    var elt = (evt.detail && evt.detail.requestConfig && evt.detail.requestConfig.elt) || null;
    if (!elt || !elt.closest || !elt.closest("#script-editor")) return;
    var status = (evt.detail.xhr && evt.detail.xhr.status) || 0;
    if (status === 403) {
      writeFail("This page is for the administrator on this computer.");
    } else if (status === 401) {
      // The guard redirects to the login; nothing to say that the new page does not say.
    } else {
      writeFail("KAMmon could not reach KAM. Nothing was changed. Try again.");
    }
  });
  document.body.addEventListener("htmx:sendError", function (evt) {
    var elt = (evt.detail && evt.detail.requestConfig && evt.detail.requestConfig.elt) || null;
    if (!elt || !elt.closest || !elt.closest("#script-editor")) return;
    writeFail("KAMmon could not reach KAM. Nothing was changed. Try again.");
  });
})();