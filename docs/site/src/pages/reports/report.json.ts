import type { APIRoute } from "astro";
import report from "../../../report-default.json";
export const GET: APIRoute = () => new Response(JSON.stringify(report.report), {
  headers: { "Content-Type": "application/json; charset=utf-8" },
});
