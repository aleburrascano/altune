// jest-expo automocks the ExpoCrypto native module, so the real randomUUID()
// returns undefined under test and every id minted from it fails its own v4
// contract. Node's CSPRNG honours that contract, so a test can hold the property
// the native module is there for — unguessable, non-repeating ids — rather than
// a stub's fixed value.
const { randomUUID } = require('node:crypto');

module.exports = { randomUUID };
