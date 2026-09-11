// Adapted from Hovel c461ba; Copyright 2026 William Born; Apache-2.0. See UPSTREAM.md.
import type { APIRoute } from "astro";
import { htmlToText, pages } from "../lib/catalog";
export const GET: APIRoute = () => new Response(JSON.stringify(pages.map((page) => ({
  description: page.description ?? "", group: page.group, href: page.href,
  text: htmlToText(page.body), title: page.navTitle ?? page.title,
}))), { headers: { "Content-Type": "application/json; charset=utf-8" } });
