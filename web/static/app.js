const form = document.querySelector("#f");
const status = document.querySelector("#s");
const submitButton = document.querySelector("#b");
const languageSelect = document.querySelector("#lang");
const themeButton = document.querySelector("#theme");

const supportedLocales = ["en", "es", "pt", "fr"];
let locale = getLocale(
  localStorage.getItem("unscribd-language") || navigator.language || "en",
);
let translations = {};
let currentJob;

function getLocale(value) {
  const code = value.toLowerCase().split("-")[0];
  return supportedLocales.includes(code) ? code : "en";
}

function translate(key, values = {}) {
  const messages = translations[locale] || translations.en || {};
  return (messages[key] || key).replace("{n}", values.n || "");
}

function renderStatus(job) {
  if (!job) {
    status.textContent = translate("idle");
    return;
  }

  status.replaceChildren();
  if (job.status === "queued") {
    const ahead = Math.max(0, (job.position || 1) - 1);
    const heading = document.createElement("strong");
    const message = document.createElement("span");

    heading.textContent = ahead ? translate("waiting") : translate("next");
    message.textContent =
      ahead === 1
        ? translate("one")
        : ahead
          ? translate("many", { n: ahead })
          : translate("start");
    status.append(heading, message);
    return;
  }

  const messages = {
    running: "running",
    complete: "ready",
    failed: "failed",
  };
  status.textContent = translate(messages[job.status] || "failed");
}

function render() {
  document.documentElement.lang = locale;
  languageSelect.value = locale;
  document.querySelectorAll("[data-t]").forEach((element) => {
    element.textContent = translate(element.dataset.t);
  });

  const darkMode = document.documentElement.dataset.theme === "dark";
  themeButton.setAttribute(
    "aria-label",
    translate(darkMode ? "light" : "dark"),
  );
  renderStatus(currentJob);
}

function setTheme(value) {
  document.documentElement.dataset.theme = value;
  localStorage.setItem("unscribd-theme", value);
  render();
}

async function loadLocale(code) {
  try {
    const response = await fetch(`/static/locales/${code}.json`);
    if (!response.ok) throw new Error("locale unavailable");

    translations[code] = await response.json();
    if (code !== "en" && !translations.en) {
      const fallback = await fetch("/static/locales/en.json");
      translations.en = await fallback.json();
    }
    render();
  } catch (error) {
    if (code !== "en") {
      locale = "en";
      loadLocale("en");
    }
  }
}

async function submitJob(event) {
  event.preventDefault();
  submitButton.disabled = true;
  status.textContent = translate("starting");

  let response;
  let job;
  try {
    response = await fetch("/jobs", {
      method: "POST",
      headers: { "content-type": "application/json" },
      body: JSON.stringify(Object.fromEntries(new FormData(form))),
    });
    const body = await response.text();
    try {
      job = JSON.parse(body);
    } catch {
      job = { detail: body.trim() };
    }
  } catch {
    status.textContent = translate("unable");
    submitButton.disabled = false;
    return;
  }

  if (!response.ok) {
    status.textContent = job.detail || translate("unable");
    submitButton.disabled = false;
    return;
  }
  watchJob(job.id);
}

async function watchJob(id) {
  const response = await fetch(`/jobs/${id}`);
  const job = await response.json();
  currentJob = job;
  renderStatus(job);

  if (job.status === "complete") {
    const download = document.createElement("a");
    download.href = `/jobs/${id}/file`;
    download.textContent = translate("retry");
    status.append(" ", download);

    const automaticDownload = download.cloneNode();
    automaticDownload.hidden = true;
    document.body.append(automaticDownload);
    automaticDownload.click();
    automaticDownload.remove();
    submitButton.disabled = false;
    return;
  }

  if (job.status === "failed") {
    submitButton.disabled = false;
    return;
  }
  setTimeout(() => watchJob(id), 1200);
}

languageSelect.addEventListener("change", () => {
  locale = getLocale(languageSelect.value);
  localStorage.setItem("unscribd-language", locale);
  loadLocale(locale);
});

themeButton.addEventListener("click", () => {
  const darkMode = document.documentElement.dataset.theme === "dark";
  setTheme(darkMode ? "light" : "dark");
});

setTheme(
  localStorage.getItem("unscribd-theme") ||
    (matchMedia("(prefers-color-scheme: dark)").matches ? "dark" : "light"),
);
form.addEventListener("submit", submitJob);
loadLocale(locale);
