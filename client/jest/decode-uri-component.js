// decode-uri-component 0.5 ships ESM only; jest loads CommonJS, so map it to this equivalent.
module.exports = { __esModule: true, default: (encoded) => decodeURIComponent(encoded) };
