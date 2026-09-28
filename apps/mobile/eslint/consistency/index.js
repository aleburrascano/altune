const fs = require('fs');
const path = require('path');

function loadRules() {
  return fs
    .readdirSync(__dirname)
    .filter((file) => file.endsWith('.js') && file !== 'index.js')
    .map((file) => require(path.join(__dirname, file)))
    .sort((a, b) => a.id.localeCompare(b.id));
}

module.exports = { loadRules };
