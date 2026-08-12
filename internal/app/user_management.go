package app

import (
	"encoding/json"
	"net/http"

	"github.com/gin-gonic/gin"
)

var userManagementAllowedPages = []string{
	"/JSON_view",
	"/jira/json",
	"/jira/reports",
	"/controls",
	"/controls/manage",
	"/security-nfrs",
	"/security-nfrs/manage",
	"/security-nfrs/links",
	"/policies",
	"/policies/manage",
	"/policies/coverage",
	"/templates",
	"/templates/manage",
	"/reports",
	"/risk-register",
	"/risk-register/manage",
	"/asset-types",
	"/exceptions",
	"/changelog",
	"/ai-chat",
	"/wiz-rules",
	"/docs",
}

func userManagementPage(c *gin.Context) {
	html := `<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>User Management · CareLock Consulting</title>
  <style>
    body { margin:0; font-family: Arial, sans-serif; background:#0b1220; color:#e5e7eb; }
    main { max-width: 1320px; margin: 24px auto; padding: 24px; background:#111827; border:1px solid #334155; border-radius:16px; }
    h1 { margin:0 0 10px; }
    p { color:#94a3b8; }
    .bar { display:flex; gap:8px; flex-wrap:wrap; margin-bottom:12px; }
    input, select, button, textarea { font:inherit; }
    input, select, textarea { padding:8px 10px; border:1px solid #334155; border-radius:10px; background:#0f172a; color:#e2e8f0; }
    button { padding:8px 12px; border-radius:10px; border:1px solid #334155; background:#1e293b; color:#e2e8f0; cursor:pointer; }
    table { width:100%; border-collapse:collapse; }
    th, td { border:1px solid #334155; padding:8px; vertical-align:top; }
    th { background:#1f2937; text-align:left; }
    .status { margin:10px 0; color:#93c5fd; }
    .pages { max-height:160px; overflow:auto; display:grid; grid-template-columns:repeat(auto-fill,minmax(220px,1fr)); gap:4px; }
    .pages label { display:flex; gap:6px; align-items:center; font-size:12px; }
  </style>
</head>
<body>
  <main>
    <div class="bar" style="justify-content:space-between;">
      <h1>User Management</h1>
      <button id="logoutBtn" type="button">Logout</button>
    </div>
    <p>Admin-only page to create, update, delete auth users and control page access.</p>
    <div class="status" id="status">Loading users...</div>
    <div class="bar">
      <input id="newUsername" placeholder="username">
      <input id="newPassword" type="password" placeholder="password (5+ chars)">
      <select id="newIsAdmin"><option value="false">User</option><option value="true">Admin</option></select>
      <button id="createBtn" type="button">Create User</button>
    </div>
    <table id="grid"></table>
  </main>
  <script>
    const allowedPages = ` + toJSONList(userManagementAllowedPages) + `;
    const statusEl = document.getElementById("status");
    const grid = document.getElementById("grid");

    function esc(v){ return (v ?? "").toString().replace(/[&<>"']/g, m => ({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'}[m])); }
    function pagesCheckboxes(selected, prefix){
      const set = new Set(selected || []);
      return '<div class="pages">' + allowedPages.map(p => '<label><input type="checkbox" data-page="'+esc(p)+'" '+(set.has(p)?'checked':'')+' data-prefix="'+prefix+'">'+esc(p)+'</label>').join('') + '</div>';
    }

    async function loadUsers(){
      const res = await fetch('/auth/users');
      if(!res.ok){ statusEl.textContent = 'Failed to load users'; return; }
      const users = await res.json();
      const head = '<tr><th>Username</th><th>Role</th><th>Password Reset</th><th>Allowed Pages</th><th>Actions</th></tr>';
      const rows = users.map(u => {
        const role = '<select data-role="'+esc(u.username)+'"><option value="false" '+(!u.is_admin?'selected':'')+'>User</option><option value="true" '+(u.is_admin?'selected':'')+'>Admin</option></select>';
        const pass = '<input type="password" placeholder="new password (optional)" data-pass="'+esc(u.username)+'">';
        const pages = pagesCheckboxes(u.allowed_pages || [], 'row-' + u.username);
        const actions = '<button data-save="'+esc(u.username)+'">Save</button> <button data-del="'+esc(u.username)+'">Delete</button>';
        return '<tr><td>'+esc(u.username)+'</td><td>'+role+'</td><td>'+pass+'</td><td>'+pages+'</td><td>'+actions+'</td></tr>';
      }).join('');
      grid.innerHTML = head + rows;
      statusEl.textContent = 'Loaded ' + users.length + ' users';
    }

    function selectedPages(prefix){
      return Array.from(document.querySelectorAll('input[data-prefix="'+prefix+'"]:checked')).map(i => i.getAttribute('data-page'));
    }

    async function createUser(){
      const username = document.getElementById('newUsername').value.trim();
      const password = document.getElementById('newPassword').value;
      const isAdmin = document.getElementById('newIsAdmin').value === 'true';
      if(!username || !password){ statusEl.textContent = 'Username and password are required'; return; }
      const res = await fetch('/auth/users', {
        method:'POST',
        headers:{'Content-Type':'application/json'},
        body: JSON.stringify({username, password, is_admin:isAdmin, allowed_pages:[]})
      });
      if(!res.ok){ statusEl.textContent = 'Create failed: ' + await res.text(); return; }
      statusEl.textContent = 'User created';
      await loadUsers();
    }

    async function saveUser(username){
      const roleEl = document.querySelector('select[data-role="'+username+'"]');
      const passEl = document.querySelector('input[data-pass="'+username+'"]');
      const payload = { is_admin: roleEl.value === 'true', allowed_pages: selectedPages('row-' + username) };
      const pass = passEl.value.trim();
      if(pass) payload.password = pass;
      const res = await fetch('/auth/users/' + encodeURIComponent(username), {
        method:'PUT',
        headers:{'Content-Type':'application/json'},
        body: JSON.stringify(payload)
      });
      if(!res.ok){ statusEl.textContent = 'Save failed: ' + await res.text(); return; }
      statusEl.textContent = 'User updated';
      await loadUsers();
    }

    async function deleteUser(username){
      const res = await fetch('/auth/users/' + encodeURIComponent(username), {method:'DELETE'});
      if(!res.ok){ statusEl.textContent = 'Delete failed: ' + await res.text(); return; }
      statusEl.textContent = 'User deleted';
      await loadUsers();
    }

    document.getElementById('createBtn').addEventListener('click', createUser);
    document.getElementById('logoutBtn').addEventListener('click', async () => {
      await fetch('/auth/logout', { method: 'POST' });
      window.location.href = '/login';
    });
    grid.addEventListener('click', async (e) => {
      const save = e.target.getAttribute('data-save');
      const del = e.target.getAttribute('data-del');
      if(save) await saveUser(save);
      if(del) await deleteUser(del);
    });
    loadUsers();
  </script>
</body>
</html>`
	c.Data(http.StatusOK, "text/html; charset=utf-8", []byte(html))
}

func toJSONList(values []string) string {
	if len(values) == 0 {
		return "[]"
	}
	encoded, err := json.Marshal(values)
	if err != nil {
		return "[]"
	}
	return string(encoded)
}
