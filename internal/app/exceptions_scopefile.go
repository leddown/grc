package app

// scopeFileApplication is the name a scope file carries for the application
// that exported it. Import Scope reads no file that names another.
const scopeFileApplication = "grc"

// exceptionScopeFileScript writes and checks the scope file of the Exceptions
// page. It touches neither the page nor its state, so it can be run on its
// own: the tests run it under node.
const exceptionScopeFileScript = `
    // A scope file is the scope of an exercise as a local JSON file: every
    // Security NFR of the catalog, each marked in scope or not. It holds no
    // exceptions. Import Scope reads a file only if it is what Export Scope of
    // this application writes, to the letter and unchanged: these fields and
    // no others, each of its type, this application's name, and a checksum
    // over the content. The checksum shows that a file was changed after it
    // was exported. It does not prove who wrote it: how it is made is on this
    // page for anyone to read.
    const SCOPE_FILE_FORMAT = 'security-nfr-scope';
    const SCOPE_FILE_VERSION = 2;
    const SCOPE_FILE_APPLICATION = '` + scopeFileApplication + `';
    const SCOPE_FILE_MAX_BYTES = 5 * 1024 * 1024;
    const SCOPE_FILE_MAX_ENTRIES = 10000;
    const SCOPE_FILE_MAX_KEY = 256;
    const SCOPE_FILE_MAX_SUMMARY = 20000;
    const SCOPE_FILE_FIELDS = ['format', 'version', 'application', 'exported_at', 'scope', 'checksum'];
    const SCOPE_ENTRY_FIELDS = ['key', 'id', 'summary', 'in_scope'];
    const SCOPE_FILE_CONTROL = /[\u0000-\u001f\u007f]/;
    const SCOPE_FILE_TIME = /^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}\.\d{3}Z$/;
    const SCOPE_FILE_CHECKSUM = /^sha256:[0-9a-f]{64}$/;

    // SHA-256 of a text's UTF-8 bytes, in hexadecimal. Written out because a
    // page served over plain HTTP has no crypto.subtle.
    function sha256Hex(text) {
      const k = [
        0x428a2f98, 0x71374491, 0xb5c0fbcf, 0xe9b5dba5, 0x3956c25b, 0x59f111f1, 0x923f82a4, 0xab1c5ed5,
        0xd807aa98, 0x12835b01, 0x243185be, 0x550c7dc3, 0x72be5d74, 0x80deb1fe, 0x9bdc06a7, 0xc19bf174,
        0xe49b69c1, 0xefbe4786, 0x0fc19dc6, 0x240ca1cc, 0x2de92c6f, 0x4a7484aa, 0x5cb0a9dc, 0x76f988da,
        0x983e5152, 0xa831c66d, 0xb00327c8, 0xbf597fc7, 0xc6e00bf3, 0xd5a79147, 0x06ca6351, 0x14292967,
        0x27b70a85, 0x2e1b2138, 0x4d2c6dfc, 0x53380d13, 0x650a7354, 0x766a0abb, 0x81c2c92e, 0x92722c85,
        0xa2bfe8a1, 0xa81a664b, 0xc24b8b70, 0xc76c51a3, 0xd192e819, 0xd6990624, 0xf40e3585, 0x106aa070,
        0x19a4c116, 0x1e376c08, 0x2748774c, 0x34b0bcb5, 0x391c0cb3, 0x4ed8aa4a, 0x5b9cca4f, 0x682e6ff3,
        0x748f82ee, 0x78a5636f, 0x84c87814, 0x8cc70208, 0x90befffa, 0xa4506ceb, 0xbef9a3f7, 0xc67178f2
      ];
      const hash = [0x6a09e667, 0xbb67ae85, 0x3c6ef372, 0xa54ff53a, 0x510e527f, 0x9b05688c, 0x1f83d9ab, 0x5be0cd19];
      const bytes = new TextEncoder().encode(text);
      const padded = new Uint8Array(Math.ceil((bytes.length + 9) / 64) * 64);
      padded.set(bytes);
      padded[bytes.length] = 0x80;
      const view = new DataView(padded.buffer);
      view.setUint32(padded.length - 8, Math.floor(bytes.length / 0x20000000));
      view.setUint32(padded.length - 4, (bytes.length * 8) >>> 0);
      const rotate = (value, by) => (value >>> by) | (value << (32 - by));
      const w = new Uint32Array(64);
      for (let block = 0; block < padded.length; block += 64) {
        for (let i = 0; i < 16; i++) w[i] = view.getUint32(block + i * 4);
        for (let i = 16; i < 64; i++) {
          const s0 = rotate(w[i - 15], 7) ^ rotate(w[i - 15], 18) ^ (w[i - 15] >>> 3);
          const s1 = rotate(w[i - 2], 17) ^ rotate(w[i - 2], 19) ^ (w[i - 2] >>> 10);
          w[i] = (w[i - 16] + s0 + w[i - 7] + s1) | 0;
        }
        let [a, b, c, d, e, f, g, h] = hash;
        for (let i = 0; i < 64; i++) {
          const t1 = (h + (rotate(e, 6) ^ rotate(e, 11) ^ rotate(e, 25)) + ((e & f) ^ (~e & g)) + k[i] + w[i]) | 0;
          const t2 = ((rotate(a, 2) ^ rotate(a, 13) ^ rotate(a, 22)) + ((a & b) ^ (a & c) ^ (b & c))) | 0;
          h = g;
          g = f;
          f = e;
          e = (d + t1) | 0;
          d = c;
          c = b;
          b = a;
          a = (t1 + t2) | 0;
        }
        [a, b, c, d, e, f, g, h].forEach((value, i) => { hash[i] = (hash[i] + value) | 0; });
      }
      return hash.map((value) => (value >>> 0).toString(16).padStart(8, '0')).join('');
    }

    function isPlainObject(value) {
      return value !== null && typeof value === 'object' && !Array.isArray(value) &&
        Object.getPrototypeOf(value) === Object.prototype;
    }

    // hasExactly is true when value has the named fields and no other.
    function hasExactly(value, fields) {
      return Object.keys(value).length === fields.length &&
        fields.every((field) => Object.prototype.hasOwnProperty.call(value, field));
    }

    function validScopeEntry(entry) {
      return isPlainObject(entry) && hasExactly(entry, SCOPE_ENTRY_FIELDS) &&
        typeof entry.key === 'string' && entry.key !== '' && entry.key.length <= SCOPE_FILE_MAX_KEY &&
        entry.key === entry.key.trim() && !SCOPE_FILE_CONTROL.test(entry.key) &&
        typeof entry.id === 'string' && entry.id.length <= SCOPE_FILE_MAX_KEY && !SCOPE_FILE_CONTROL.test(entry.id) &&
        typeof entry.summary === 'string' && entry.summary.length <= SCOPE_FILE_MAX_SUMMARY &&
        typeof entry.in_scope === 'boolean';
    }

    function validScopeTime(value) {
      if (typeof value !== 'string' || !SCOPE_FILE_TIME.test(value)) return false;
      const time = Date.parse(value);
      return Number.isFinite(time) && new Date(time).toISOString() === value;
    }

    // The checksum is over the content in one fixed order, so it does not
    // depend on how the file is laid out. file has to have passed the checks
    // on its fields first.
    function scopeFileChecksum(file) {
      return 'sha256:' + sha256Hex(JSON.stringify([
        file.format,
        file.version,
        file.application,
        file.exported_at,
        file.scope.map((entry) => [entry.key, entry.id, entry.summary, entry.in_scope])
      ]));
    }

    // scopeFileTextOf is the file for entries, each a key, an id, a summary
    // and whether the NFR is in scope.
    function scopeFileTextOf(entries, exportedAt) {
      const file = {
        format: SCOPE_FILE_FORMAT,
        version: SCOPE_FILE_VERSION,
        application: SCOPE_FILE_APPLICATION,
        exported_at: exportedAt,
        scope: entries.map((entry) => ({
          key: entry.key,
          id: entry.id,
          summary: entry.summary,
          in_scope: entry.in_scope
        }))
      };
      file.checksum = scopeFileChecksum(file);
      return JSON.stringify(file, null, 2) + '\n';
    }

    // scopeFileRefusal says why a chosen file is not read at all, or nothing.
    function scopeFileRefusal(name, size, type) {
      if (!/\.json$/i.test(String(name || ''))) return 'the file is not a .json file.';
      if (!(size > 0)) return 'the file is empty.';
      if (size > SCOPE_FILE_MAX_BYTES) return 'the file is larger than a scope file can be.';
      if (type && type !== 'application/json') return 'the file is not a JSON file.';
      return '';
    }

    // readScopeFile reads text as a scope file exported from this application
    // for a catalog with these keys. It gives { error } and changes nothing
    // when the text is anything else, and otherwise { inScope, unlisted }: the
    // keys in scope, and how many NFRs of the catalog the file does not list.
    // Nothing of a refused file is read further or shown.
    function readScopeFile(text, catalog) {
      const refuse = (error) => ({ error });
      if (typeof text !== 'string' || text.length > SCOPE_FILE_MAX_BYTES) {
        return refuse('the file is larger than a scope file can be.');
      }
      let file;
      try {
        file = JSON.parse(text);
      } catch (error) {
        return refuse('the file is not JSON.');
      }
      if (!isPlainObject(file) || file.format !== SCOPE_FILE_FORMAT || !Number.isInteger(file.version)) {
        return refuse('the file is not a scope file.');
      }
      if (file.version !== SCOPE_FILE_VERSION) {
        return refuse(file.version > 0 && file.version < 1000
          ? 'the file is a scope file of version ' + file.version + ' and this application reads version ' + SCOPE_FILE_VERSION + '. Export the scope again.'
          : 'the file is not a scope file.');
      }
      if (!hasExactly(file, SCOPE_FILE_FIELDS)) {
        return refuse('the file does not have the fields of a scope file.');
      }
      if (file.application !== SCOPE_FILE_APPLICATION) {
        return refuse('the file was not exported from this application.');
      }
      if (!validScopeTime(file.exported_at)) {
        return refuse('the time the file was exported is not valid.');
      }
      if (typeof file.checksum !== 'string' || !SCOPE_FILE_CHECKSUM.test(file.checksum)) {
        return refuse('the file has no valid checksum.');
      }
      if (!Array.isArray(file.scope) || file.scope.length > SCOPE_FILE_MAX_ENTRIES) {
        return refuse('the list of Security NFRs in the file is not valid.');
      }
      const listed = new Set();
      for (let i = 0; i < file.scope.length; i++) {
        const entry = file.scope[i];
        if (!validScopeEntry(entry)) {
          return refuse('entry ' + (i + 1) + ' of the file is not a Security NFR as this application exports one.');
        }
        if (listed.has(entry.key)) {
          return refuse('entry ' + (i + 1) + ' of the file names a Security NFR the file has already named.');
        }
        listed.add(entry.key);
      }
      if (scopeFileChecksum(file) !== file.checksum) {
        return refuse('the file has been changed since it was exported.');
      }
      const known = new Set(catalog);
      const unknown = file.scope.filter((entry) => !known.has(entry.key)).length;
      if (unknown) {
        return refuse((unknown === 1
          ? '1 Security NFR in the file is not in the catalog.'
          : unknown + ' Security NFRs in the file are not in the catalog.') +
          ' The catalog has changed since the file was exported, or the file is from another catalog. Export the scope again.');
      }
      return {
        inScope: file.scope.filter((entry) => entry.in_scope).map((entry) => entry.key),
        unlisted: catalog.filter((key) => !listed.has(key)).length
      };
    }
`
