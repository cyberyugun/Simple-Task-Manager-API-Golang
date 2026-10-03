import http from "k6/http";
import { check, sleep } from "k6";

const baseURL = __ENV.BASE_URL || "http://127.0.0.1:8080";
const profile = __ENV.PROFILE || "smoke";
const runID = __ENV.RUN_ID || "local";

const profiles = {
  smoke: {
    executor: "constant-vus",
    vus: 2,
    duration: "10s",
  },
  load: {
    executor: "ramping-vus",
    startVUs: 0,
    stages: [
      { duration: "30s", target: 10 },
      { duration: "2m", target: 25 },
      { duration: "30s", target: 0 },
    ],
  },
  spike: {
    executor: "ramping-vus",
    startVUs: 0,
    stages: [
      { duration: "20s", target: 10 },
      { duration: "20s", target: 75 },
      { duration: "40s", target: 75 },
      { duration: "20s", target: 10 },
      { duration: "20s", target: 0 },
    ],
  },
  soak: {
    executor: "constant-vus",
    vus: 25,
    duration: "15m",
  },
};

if (!profiles[profile]) {
  throw new Error("PROFILE must be one of smoke, load, spike, or soak");
}

export const options = {
  scenarios: {
    task_flow: profiles[profile],
  },
  thresholds: {
    checks: ["rate>0.99"],
    http_req_failed: ["rate<0.01"],
    http_req_duration: ["p(95)<750", "p(99)<1500"],
    "http_req_duration{name:list_tasks}": ["p(95)<500"],
    "http_req_duration{name:create_task}": ["p(95)<750"],
  },
};

function jsonHeaders(token) {
  return {
    headers: {
      Authorization: `Bearer ${token}`,
      "Content-Type": "application/json",
    },
  };
}

export function setup() {
  const email = `perf-${runID}-${Date.now()}@example.com`;
  const response = http.post(
    `${baseURL}/api/auth/register`,
    JSON.stringify({
      name: "Performance User",
      email,
      password: "performance-password-123",
    }),
    {
      headers: { "Content-Type": "application/json" },
      tags: { name: "register_setup" },
    },
  );

  const ok = check(response, {
    "performance user registered": (r) => r.status === 201,
  });
  if (!ok) {
    throw new Error(`registration failed: status=${response.status} body=${response.body}`);
  }

  const payload = JSON.parse(response.body);
  return {
    accessToken: payload.data.access_token,
  };
}

export default function (data) {
  const auth = jsonHeaders(data.accessToken);

  const list = http.get(`${baseURL}/api/tasks?page=1&limit=20&sort=created_at&order=desc`, {
    headers: auth.headers,
    tags: { name: "list_tasks" },
  });
  check(list, {
    "list tasks 200": (r) => r.status === 200,
  });

  const create = http.post(
    `${baseURL}/api/tasks`,
    JSON.stringify({
      title: `load-task-${__VU}-${__ITER}`,
      description: "k6 performance regression task",
    }),
    {
      headers: auth.headers,
      tags: { name: "create_task" },
    },
  );
  const created = check(create, {
    "create task 201": (r) => r.status === 201,
  });

  if (created) {
    const task = JSON.parse(create.body).data;

    const complete = http.patch(
      `${baseURL}/api/tasks/${task.id}/complete`,
      null,
      {
        headers: auth.headers,
        tags: { name: "complete_task" },
      },
    );
    check(complete, {
      "complete task 200": (r) => r.status === 200,
    });

    const get = http.get(`${baseURL}/api/tasks/${task.id}`, {
      headers: auth.headers,
      tags: { name: "get_task" },
    });
    check(get, {
      "get task 200": (r) => r.status === 200,
    });

    const remove = http.del(`${baseURL}/api/tasks/${task.id}`, null, {
      headers: auth.headers,
      tags: { name: "delete_task" },
    });
    check(remove, {
      "delete task 200": (r) => r.status === 200,
    });
  }

  sleep(0.1);
}

export function handleSummary(data) {
  return {
    "performance-summary.json": JSON.stringify(data, null, 2),
  };
}
