import { inventory } from "../../lib/api";

// A static build artifact, not a running API service.
export const GET = () => new Response(JSON.stringify(inventory), {
  headers: { "Content-Type": "application/json; charset=utf-8" },
});
