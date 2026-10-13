package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// scopeFileHarness runs after exceptionScopeFileScript and a line that sets
// vectors, the texts and the digests Go gives for them. It reads a file as
// exported, and then every way a file can be something else.
const scopeFileHarness = `
const failures = [];
let checks = 0;
const check = (name, ok, detail) => {
  checks++;
  if (!ok) failures.push(name + (detail ? ': ' + detail : ''));
};

for (const [text, want] of vectors) {
  check('sha256 of ' + text.length + ' characters', sha256Hex(text) === want, sha256Hex(text) + ' want ' + want);
}

const catalog = ['1', '2', '3', '10'];
const entries = [
  { key: '1', id: '0.1', summary: 'Architecture is documented', in_scope: true },
  { key: '2', id: '1.1', summary: 'Line one\nline two, "quoted", <b>,   and 😀', in_scope: false },
  { key: '3', id: '', summary: '', in_scope: true },
  { key: '10', id: '2.1', summary: 'Boundaries are filtered', in_scope: false }
];
const at = '2026-10-13T10:00:00.000Z';
const good = scopeFileTextOf(entries, at);

const read = readScopeFile(good, catalog);
check('a file as exported is read', !read.error && JSON.stringify(read.inScope) === '["1","3"]' && read.unlisted === 0, JSON.stringify(read));

const exported = JSON.parse(good);
check('the file has the six fields and no other', JSON.stringify(Object.keys(exported)) === JSON.stringify(SCOPE_FILE_FIELDS));
check('the file names this application', exported.application === SCOPE_FILE_APPLICATION && exported.application !== '');
check('the file holds no exceptions', !/exception|partial|weight/i.test(good));

const relaid = readScopeFile(JSON.stringify(JSON.parse(good)), catalog);
check('the same content laid out otherwise is read', !relaid.error && relaid.inScope.length === 2, JSON.stringify(relaid));

const grown = readScopeFile(good, catalog.concat(['11', '12']));
check('NFRs added to the catalog since are counted as not listed', !grown.error && grown.unlisted === 2, JSON.stringify(grown));

const refused = (name, text, want, keys) => {
  const got = readScopeFile(text, keys || catalog);
  check(name, typeof got.error === 'string' && got.error.includes(want) && got.inScope === undefined, JSON.stringify(got));
};

// change edits a copy of the good file. With restamp the checksum is made to
// fit again, so that it is the rule under test that refuses the file and not
// the checksum.
const change = (edit, restamp) => {
  const file = JSON.parse(good);
  edit(file);
  if (restamp) {
    try {
      file.checksum = scopeFileChecksum(file);
    } catch (error) {
      // An entry that is not an object cannot be summed; it is refused sooner.
    }
  }
  return JSON.stringify(file);
};

refused('not JSON', 'format: security-nfr-scope', 'not JSON');
refused('an empty text', '', 'not JSON');
for (const text of ['[]', 'null', '42', '"security-nfr-scope"', '{}', 'true']) {
  refused('JSON that is not a scope file: ' + text, text, 'not a scope file');
}
refused('another format', change((f) => { f.format = 'something-else'; }, true), 'not a scope file');
refused('a version given as text', change((f) => { f.version = '2'; }, true), 'not a scope file');
refused('a version that is not whole', change((f) => { f.version = 2.5; }, true), 'not a scope file');
refused('a negative version', change((f) => { f.version = -1; }, true), 'not a scope file');
refused('a later version', change((f) => { f.version = 3; }, true), 'version 3');
refused('the scope file of version 1',
  JSON.stringify({ format: 'security-nfr-scope', version: 1, exported_at: at, scope: [{ key: '1', id: '', summary: '', in_scope: true }] }),
  'version 1 and this application reads version 2');

for (const field of ['application', 'exported_at', 'scope', 'checksum']) {
  refused('no ' + field, change((f) => { delete f[field]; }), 'does not have the fields');
}
refused('a field too many', change((f) => { f.exceptions = []; }), 'does not have the fields');
refused('a __proto__ field', good.replace('{', '{"__proto__": {"polluted": true},'), 'does not have the fields');
check('nothing was polluted', ({}).polluted === undefined);

refused('another application', change((f) => { f.application = 'another-application'; }, true), 'not exported from this application');
refused('an application that is not text', change((f) => { f.application = 5; }, true), 'not exported from this application');

for (const time of ['yesterday', '2026-10-13', '2026-10-13T10:00:00Z', '2026-02-30T10:00:00.000Z', '2026-13-01T10:00:00.000Z', 1760349600000, null]) {
  refused('export time ' + JSON.stringify(time), change((f) => { f.exported_at = time; }, true), 'time the file was exported');
}

for (const sum of ['', 'abc', 'sha256:' + 'g'.repeat(64), 'sha256:' + 'A'.repeat(64), 'sha256:' + 'a'.repeat(63), 'md5:' + 'a'.repeat(32), 7, null]) {
  refused('checksum ' + JSON.stringify(sum), change((f) => { f.checksum = sum; }), 'no valid checksum');
}

for (const list of [{}, 'everything', null, 4]) {
  refused('a scope that is ' + JSON.stringify(list), change((f) => { f.scope = list; }), 'list of Security NFRs');
}
refused('more entries than a catalog has', change((f) => {
  f.scope = [];
  for (let i = 0; i <= SCOPE_FILE_MAX_ENTRIES; i++) f.scope.push({ key: 'k' + i, id: '', summary: '', in_scope: false });
}, true), 'list of Security NFRs');

const entryRefused = (name, edit) => {
  refused('entry: ' + name, change((f) => { edit(f.scope); }, true), 'entry 2 of the file is not a Security NFR');
};
entryRefused('null', (scope) => { scope[1] = null; });
entryRefused('a bare key', (scope) => { scope[1] = '2'; });
entryRefused('a list', (scope) => { scope[1] = ['2', '', '', true]; });
entryRefused('empty', (scope) => { scope[1] = {}; });
entryRefused('a field too many', (scope) => { scope[1].exception = 'Full'; });
entryRefused('no in_scope', (scope) => { delete scope[1].in_scope; });
entryRefused('no summary', (scope) => { delete scope[1].summary; });
entryRefused('an empty key', (scope) => { scope[1].key = ''; });
entryRefused('a key with a space before it', (scope) => { scope[1].key = ' 2'; });
entryRefused('a key with a space after it', (scope) => { scope[1].key = '2 '; });
entryRefused('a key that is a number', (scope) => { scope[1].key = 2; });
entryRefused('a key with a control character', (scope) => { scope[1].key = '2\u0000'; });
entryRefused('a key with a line break', (scope) => { scope[1].key = '2\n'; });
entryRefused('a key too long', (scope) => { scope[1].key = 'k'.repeat(SCOPE_FILE_MAX_KEY + 1); });
entryRefused('an id that is a number', (scope) => { scope[1].id = 1.1; });
entryRefused('an id with a control character', (scope) => { scope[1].id = '1.1\u001f'; });
entryRefused('an id too long', (scope) => { scope[1].id = '1'.repeat(SCOPE_FILE_MAX_KEY + 1); });
entryRefused('a summary that is null', (scope) => { scope[1].summary = null; });
entryRefused('a summary that is a list', (scope) => { scope[1].summary = ['text']; });
entryRefused('a summary too long', (scope) => { scope[1].summary = 's'.repeat(SCOPE_FILE_MAX_SUMMARY + 1); });
entryRefused('in_scope as text', (scope) => { scope[1].in_scope = 'true'; });
entryRefused('in_scope as a number', (scope) => { scope[1].in_scope = 1; });
entryRefused('in_scope as null', (scope) => { scope[1].in_scope = null; });

refused('a key twice', change((f) => { f.scope[2].key = '1'; }, true), 'entry 3 of the file names a Security NFR the file has already named');

// The file is as exported in every field, but its content was edited.
const edited = (name, edit) => {
  refused('edited: ' + name, change(edit), 'changed since it was exported');
};
edited('an NFR put in scope', (f) => { f.scope[1].in_scope = true; });
edited('an NFR taken out of scope', (f) => { f.scope[0].in_scope = false; });
edited('a summary', (f) => { f.scope[0].summary += '.'; });
edited('an id', (f) => { f.scope[0].id = '0.2'; });
edited('a key', (f) => { f.scope[3].key = '11'; });
edited('the time', (f) => { f.exported_at = '2026-10-13T10:00:00.001Z'; });
edited('the order', (f) => { f.scope.reverse(); });
edited('an entry removed', (f) => { f.scope.pop(); });
edited('an entry added', (f) => { f.scope.push({ key: '11', id: '', summary: '', in_scope: true }); });
edited('another checksum', (f) => { f.checksum = 'sha256:' + '0'.repeat(64); });

refused('an NFR the catalog does not have', change((f) => { f.scope[3].key = '99'; }, true), '1 Security NFR in the file is not in the catalog');
refused('a catalog that has lost two NFRs', good, '2 Security NFRs in the file are not in the catalog', ['1', '3']);
refused('a text too long', ' '.repeat(SCOPE_FILE_MAX_BYTES + 1), 'larger than a scope file can be');
refused('not a text', { format: SCOPE_FILE_FORMAT }, 'larger than a scope file can be');

// What is checked of a chosen file before it is read.
check('a .json file is read', scopeFileRefusal('security-nfr-scope-2026-10-13.json', 4096, 'application/json') === '');
check('a .JSON file of no declared type is read', scopeFileRefusal('SCOPE.JSON', 4096, '') === '');
check('another extension', scopeFileRefusal('scope.txt', 4096, 'text/plain').includes('.json file'));
check('no extension', scopeFileRefusal('scope', 4096, '').includes('.json file'));
check('json in the middle of a name', scopeFileRefusal('scope.json.exe', 4096, '').includes('.json file'));
check('no name', scopeFileRefusal('', 4096, '').includes('.json file'));
check('an empty file', scopeFileRefusal('scope.json', 0, 'application/json').includes('empty'));
check('a file too large', scopeFileRefusal('scope.json', SCOPE_FILE_MAX_BYTES + 1, 'application/json').includes('larger'));
check('a file at the limit', scopeFileRefusal('scope.json', SCOPE_FILE_MAX_BYTES, 'application/json') === '');
check('another declared type', scopeFileRefusal('scope.json', 4096, 'text/html').includes('not a JSON file'));

if (failures.length) {
  console.error(failures.join('\n'));
  process.exit(1);
}
console.log('ok ' + checks);
`

// TestScopeFileScriptUnderNode runs the page's scope file script as a browser
// would: its SHA-256 against Go's, and Import Scope's checks against a file
// as exported and against everything else. It needs node on PATH and is
// skipped without it.
func TestScopeFileScriptUnderNode(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("no node on PATH: the scope file script is not run")
	}

	texts := []string{
		"",
		"abc",
		"The quick brown fox jumps over the lazy dog",
		// Either side of the padding's block boundaries.
		strings.Repeat("a", 55),
		strings.Repeat("a", 56),
		strings.Repeat("a", 63),
		strings.Repeat("a", 64),
		strings.Repeat("a", 65),
		strings.Repeat("a", 119),
		strings.Repeat("a", 120),
		strings.Repeat("é😀  <b>&\"\n", 400),
		strings.Repeat("0123456789", 20000),
	}
	vectors := make([][2]string, 0, len(texts))
	for _, text := range texts {
		sum := sha256.Sum256([]byte(text))
		vectors = append(vectors, [2]string{text, hex.EncodeToString(sum[:])})
	}
	raw, err := json.Marshal(vectors)
	if err != nil {
		t.Fatal(err)
	}

	path := filepath.Join(t.TempDir(), "scopefile.js")
	script := exceptionScopeFileScript + "\nconst vectors = " + string(raw) + ";\n" + scopeFileHarness
	if err := os.WriteFile(path, []byte(script), 0o600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	output, err := exec.CommandContext(ctx, node, path).CombinedOutput()
	if err != nil {
		t.Fatalf("the scope file script failed under node: %v\n%s", err, output)
	}
	if !strings.HasPrefix(string(output), "ok ") {
		t.Fatalf("the scope file script did not finish: %s", output)
	}
	t.Logf("node: %s", strings.TrimSpace(string(output)))
}

// The application's name goes into a script and into a file.
func TestScopeFileApplicationIsAPlainName(t *testing.T) {
	if scopeFileApplication == "" {
		t.Fatal("the scope file has to name the application that exported it")
	}
	for _, r := range scopeFileApplication {
		if !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '_' || r == '-') {
			t.Fatalf("scopeFileApplication %q must be lower-case letters, digits, _ or -", scopeFileApplication)
		}
	}
	if !strings.Contains(exceptionScopeFileScript, "const SCOPE_FILE_APPLICATION = '"+scopeFileApplication+"';") {
		t.Error("the script does not carry the application's name")
	}
}
