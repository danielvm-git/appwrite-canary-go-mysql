// Minimal harness for the MySQL-backed Todo API.
//
// It is NOT the shared shell UI (see README): it exists so the data path can be
// exercised from a browser against the deployed Function, with Appwrite Auth for
// identity and no database driver in the browser.

const cfg = window.__TODO_CONFIG__ || {};
const $ = (sel, root = document) => root.querySelector(sel);

const state = {
  account: null,
  user: null,
  tasks: [],
  lists: [],
};

function notice(message, kind = "info") {
  const el = $("#notice");
  el.textContent = message;
  el.className = `notice ${kind}`;
  el.hidden = false;
  clearTimeout(notice.timer);
  notice.timer = setTimeout(() => (el.hidden = true), 4000);
}

async function sdk() {
  const mod = await import(cfg.appwriteSdkUrl || "https://esm.sh/appwrite@22");
  return mod;
}

async function callApi(path, { method = "GET", body } = {}) {
  if (!state.user) throw new Error("not signed in");
  const { Functions } = await sdk();
  const client = new (await sdk()).Client()
    .setEndpoint(cfg.endpoint)
    .setProject(cfg.projectId);

  // The Function is the only thing that can reach MySQL, so every data call goes
  // through execution. Appwrite injects x-appwrite-user-id from the session.
  const functions = new Functions(client);
  const execution = await functions.createExecution({
    functionId: cfg.functionId,
    path: path,
    method: method,
    body: body ? JSON.stringify(body) : "",
    async: false,
  });

  const raw = execution.responseBody || "";
  let payload;
  try {
    payload = raw ? JSON.parse(raw) : {};
  } catch {
    throw new Error(`function returned non-JSON (${execution.responseStatusCode}): ${raw.slice(0, 200)}`);
  }
  if (execution.responseStatusCode >= 400 || payload.ok === false) {
    throw new Error(payload.message || `request failed (${execution.responseStatusCode})`);
  }
  return payload;
}

async function signIn(email, password) {
  const { Client, Account } = await sdk();
  const client = new Client().setEndpoint(cfg.endpoint).setProject(cfg.projectId);
  state.account = new Account(client);
  await state.account.createEmailPasswordSession({ email, password });
  state.user = await state.account.get();
}

async function reload() {
  const scope = $("#scope").value;
  const q = $("#search").value.trim();
  const query = new URLSearchParams({ scope });
  if (q) query.set("q", q);

  const [tasks, lists] = await Promise.all([
    callApi(`/tasks?${query}`),
    callApi("/lists"),
  ]);
  state.tasks = tasks.tasks || [];
  state.lists = lists.lists || [];
  render();
}

function render() {
  $("#who").textContent = state.user ? `${state.user.name || state.user.email} · ${state.user.email}` : "";

  const tbody = $("#tasks tbody");
  tbody.innerHTML = "";
  for (const task of state.tasks) {
    const tr = document.createElement("tr");
    tr.innerHTML = `
      <td>#${task.$sequence ?? ""}</td>
      <td>${escapeHtml(task.title)}${task.notes ? `<div class="muted small">${escapeHtml(task.notes)}</div>` : ""}</td>
      <td>
        <select data-status="${task.$id}">
          ${["open", "doing", "review", "done"]
            .map((s) => `<option value="${s}"${s === task.status ? " selected" : ""}>${s}</option>`)
            .join("")}
        </select>
      </td>
      <td>${task.priority || "—"}</td>
      <td>${(task.sharedNames || []).map(escapeHtml).join(", ") || "—"}</td>
      <td class="actions">
        <button data-share="${task.$id}" type="button">share</button>
        <button data-delete="${task.$id}" type="button" class="danger">delete</button>
      </td>`;
    tbody.appendChild(tr);
  }

  $("#lists").innerHTML = "";
  for (const list of state.lists) {
    const li = document.createElement("li");
    li.innerHTML = `<span class="dot" style="background:${escapeHtml(list.color)}"></span>
      ${escapeHtml(list.name)} <button data-list="${list.$id}" type="button">×</button>`;
    $("#lists").appendChild(li);
  }
}

function escapeHtml(value) {
  return String(value ?? "").replace(/[&<>"']/g, (c) => `&#${c.charCodeAt(0)};`);
}

function wire() {
  $("#signin-form").addEventListener("submit", async (event) => {
    event.preventDefault();
    const form = new FormData(event.target);
    try {
      await signIn(form.get("email"), form.get("password"));
      $("#signin").hidden = true;
      $("#app").hidden = false;
      await reload();
    } catch (err) {
      const el = $("#signin-error");
      el.textContent = err.message || String(err);
      el.hidden = false;
    }
  });

  $("#task-form").addEventListener("submit", async (event) => {
    event.preventDefault();
    const form = new FormData(event.target);
    try {
      await callApi("/tasks", {
        method: "POST",
        body: { title: form.get("title"), priority: form.get("priority") || undefined },
      });
      event.target.reset();
      await reload();
    } catch (err) {
      const el = $("#task-error");
      el.textContent = err.message;
      el.hidden = false;
    }
  });

  $("#list-form").addEventListener("submit", async (event) => {
    event.preventDefault();
    const form = new FormData(event.target);
    try {
      await callApi("/lists", {
        method: "POST",
        body: { name: form.get("name"), color: form.get("color") || undefined },
      });
      event.target.reset();
      await reload();
    } catch (err) {
      notice(err.message, "error");
    }
  });

  $("#tasks").addEventListener("change", async (event) => {
    const id = event.target.dataset.status;
    if (!id) return;
    try {
      await callApi(`/tasks/${id}`, { method: "PATCH", body: { status: event.target.value } });
      notice("status updated");
    } catch (err) {
      notice(err.message, "error");
    }
  });

  $("#tasks").addEventListener("click", async (event) => {
    const shareId = event.target.dataset.share;
    const deleteId = event.target.dataset.delete;
    if (shareId) {
      const email = window.prompt("Share with (email)");
      if (!email) return;
      try {
        await callApi("/share", { method: "POST", body: { taskId: shareId, email } });
        notice("shared");
        await reload();
      } catch (err) {
        notice(err.message, "error");
      }
    }
    if (deleteId) {
      try {
        await callApi(`/tasks/${deleteId}`, { method: "DELETE" });
        await reload();
      } catch (err) {
        notice(err.message, "error");
      }
    }
  });

  $("#lists").addEventListener("click", async (event) => {
    const id = event.target.dataset.list;
    if (!id) return;
    try {
      await callApi(`/lists/${id}`, { method: "DELETE" });
      await reload();
    } catch (err) {
      notice(err.message, "error");
    }
  });

  $("#reload").addEventListener("click", () => reload().catch((err) => notice(err.message, "error")));
  $("#scope").addEventListener("change", () => reload().catch((err) => notice(err.message, "error")));
  $("#search").addEventListener("input", debounce(() => reload().catch(() => {}), 350));
}

function debounce(fn, ms) {
  let timer;
  return (...args) => {
    clearTimeout(timer);
    timer = setTimeout(() => fn(...args), ms);
  };
}

(async function boot() {
  wire();
  if (!cfg.endpoint || !cfg.projectId || !cfg.functionId) {
    notice("config.js is missing endpoint/projectId/functionId", "error");
    $("#signin").hidden = false;
    return;
  }
  try {
    const { Client, Account } = await sdk();
    const client = new Client().setEndpoint(cfg.endpoint).setProject(cfg.projectId);
    state.account = new Account(client);
    state.user = await state.account.get();
    $("#app").hidden = false;
    await reload();
  } catch {
    $("#signin").hidden = false;
  }
})();
