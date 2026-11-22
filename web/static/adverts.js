// Copy-to-clipboard for the advert plan. It listens on the document so the button keeps working
// after htmx swaps the live region.
document.addEventListener('click', function (event) {
  var button = event.target.closest('[data-copy-target]');
  if (!button) return;
  var source = document.getElementById(button.getAttribute('data-copy-target'));
  if (!source) return;
  var label = button.lastChild;
  if (!label || label.nodeType !== 3) {
    label = document.createElement('span');
    button.appendChild(label);
  }
  var text = source.textContent;
  var done = function (message) {
    var previous = label.textContent;
    label.textContent = message;
    setTimeout(function () { label.textContent = previous; }, 1600);
  };
  if (navigator.clipboard && window.isSecureContext) {
    navigator.clipboard.writeText(text).then(function () { done('Copied'); }, function () { done('Copy failed'); });
    return;
  }
  // Plain-HTTP LAN pages are not a secure context, so fall back to selecting the text.
  var range = document.createRange();
  range.selectNodeContents(source);
  var selection = window.getSelection();
  selection.removeAllRanges();
  selection.addRange(range);
  done('Selected — press Ctrl+C');
});
