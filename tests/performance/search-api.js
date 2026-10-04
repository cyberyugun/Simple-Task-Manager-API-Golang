import http from "k6/http";
import { check, sleep } from "k6";
import { Trend } from "k6/metrics";

export const options = {
  vus: Number(__ENV.VUS || 5),
  duration: __ENV.DURATION || "30s",
  thresholds: {
    http_req_failed: ["rate<0.01"],
    http_req_duration: ["p(95)<750"],
  },
};

const searchLatency = new Trend("search_latency");
const dashboardLatency = new Trend("dashboard_latency");

const baseURL = __ENV.BASE_URL || "http://127.0.0.1:8080";
const token = __ENV.ACCESS_TOKEN || "";

export default function () {
  const params = {
    headers: token ? { Authorization: `Bearer ${token}` } : {},
  };

  const search = http.get(`${baseURL}/api/search/tasks?q=task&limit=20`, params);
  searchLatency.add(search.timings.duration);
  check(search, {
    "search status is 200": (r) => r.status === 200,
  });

  const dashboard = http.get(`${baseURL}/api/analytics/dashboard?days=30`, params);
  dashboardLatency.add(dashboard.timings.duration);
  check(dashboard, {
    "dashboard status is 200": (r) => r.status === 200,
  });

  sleep(0.5);
}
