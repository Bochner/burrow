/* Adapted from Hovel c461ba282a8aecc7aa3a079a4613bf5e2640c388; Copyright 2026 William Born; Apache-2.0. Changed for Burrow; see docs/site/UPSTREAM.md. */
import { apiFragments } from "./api";
import report from "../../report-default.json";
export type DocArea = "home" | "book" | "modules" | "api" | "reports";

export interface DocMetadata {
  title: string;
  order: number;
  group: string;
  navTitle?: string;
  description?: string;
  moduleOrder?: number;
  moduleStatus?: string;
  moduleType?: string;
}

export interface DocPage extends DocMetadata {
  area: DocArea;
  body: string;
  href: string;
  slug: string;
  sourcePath: string;
  summary: string;
}

export interface NavGroup {
  label: string;
  pages: DocPage[];
}

export interface BookChapter {
  label: string;
  number: number;
  page: DocPage;
  partLabel: string;
  partNumber: number;
}

export interface BookPart {
  label: string;
  number: number;
  chapters: BookChapter[];
}

const HEADER = "<!-- burrow-doc:";
const HTML_ENTITIES: Record<string, string> = {
  "&amp;": "&",
  "&gt;": ">",
  "&lt;": "<",
  "&nbsp;": " ",
  "&quot;": '"',
  "&#39;": "'",
};
const rawPages = import.meta.glob<string>("../content/**/*.html", {
  eager: true,
  import: "default",
  query: "?raw",
});

function fail(sourcePath: string, message: string): never {
  throw new Error(`${sourcePath}: ${message}`);
}

export function htmlToText(html: string): string {
  return html
    .replace(/<script\b[^>]*>[\s\S]*?<\/script>/gi, " ")
    .replace(/<style\b[^>]*>[\s\S]*?<\/style>/gi, " ")
    .replace(/<[^>]+>/g, " ")
    .replace(/&(amp|gt|lt|nbsp|quot|#39);/g, (entity) => HTML_ENTITIES[entity] ?? entity)
    .replace(/\s+/g, " ")
    .trim();
}

function summarize(body: string): string {
  const paragraphs = body.matchAll(/<p\b[^>]*>([\s\S]*?)<\/p>/gi);
  for (const paragraph of paragraphs) {
    const text = htmlToText(paragraph[1]);
    if (text.length >= 48) return truncateSummary(text);
  }
  return truncateSummary(htmlToText(body));
}

function truncateSummary(text: string): string {
  if (text.length <= 220) return text;
  const shortened = text.slice(0, 217);
  const lastSpace = shortened.lastIndexOf(" ");
  return `${shortened.slice(0, lastSpace > 160 ? lastSpace : 217)}…`;
}

function parsePage(sourcePath: string, raw: string): DocPage {
  const start = raw.search(/\S/);
  const source = start < 0 ? "" : raw.slice(start);
  if (!source.startsWith(HEADER)) {
    fail(sourcePath, `first non-whitespace content must be ${HEADER} {...} -->`);
  }

  const headerEnd = source.indexOf("-->", HEADER.length);
  if (headerEnd < 0) {
    fail(sourcePath, "metadata comment is not closed");
  }

  let metadata: unknown;
  try {
    metadata = JSON.parse(source.slice(HEADER.length, headerEnd).trim());
  } catch (error) {
    fail(sourcePath, `invalid metadata JSON: ${String(error)}`);
  }
  if (metadata === null || typeof metadata !== "object" || Array.isArray(metadata)) {
    fail(sourcePath, "metadata must be a JSON object");
  }

  const candidate = metadata as Partial<DocMetadata>;
  if (typeof candidate.title !== "string" || candidate.title.trim() === "") {
    fail(sourcePath, "metadata.title must be a non-empty string");
  }
  if (!Number.isInteger(candidate.order) || (candidate.order ?? -1) < 0) {
    fail(sourcePath, "metadata.order must be a non-negative integer");
  }
  if (typeof candidate.group !== "string" || candidate.group.trim() === "") {
    fail(sourcePath, "metadata.group must be a non-empty string");
  }
  if (candidate.navTitle !== undefined && typeof candidate.navTitle !== "string") {
    fail(sourcePath, "metadata.navTitle must be a string when present");
  }
  if (candidate.description !== undefined && typeof candidate.description !== "string") {
    fail(sourcePath, "metadata.description must be a string when present");
  }
  if (candidate.moduleOrder !== undefined && (!Number.isInteger(candidate.moduleOrder) || candidate.moduleOrder < 0)) {
    fail(sourcePath, "metadata.moduleOrder must be a non-negative integer when present");
  }
  for (const field of ["moduleStatus", "moduleType"] as const) {
    if (candidate[field] !== undefined && (typeof candidate[field] !== "string" || candidate[field].trim() === "")) {
      fail(sourcePath, `metadata.${field} must be a non-empty string when present`);
    }
  }

  const body = source
    .slice(headerEnd + 3)
    .trim();
  if (!/<h1(?:\s|>)/i.test(body)) {
    fail(sourcePath, "content must contain an h1");
  }
  for (const forbidden of ["<html", "<head", "<body", "class=\"topbar\"", "class=\"sidebar\""]) {
    if (body.toLowerCase().includes(forbidden.toLowerCase())) {
      fail(sourcePath, `content fragment contains generated chrome: ${forbidden}`);
    }
  }

  const relative = sourcePath.replace(/^\.\.\/content\//, "").replace(/\.html$/, "");
  const slug = relative === "index" ? "index" : relative;
  const area: DocArea = slug === "index" ? "home" : slug.startsWith("spec/") ? "book" : slug.startsWith("api/") ? "api" : slug.startsWith("reports/") ? "reports" : "modules";

  return {
    area,
    body,
    description: candidate.description,
    group: candidate.group,
    href: slug === "index" ? "index.html" : slug.endsWith("/index") ? `${slug.slice(0, -"index".length)}` : `${slug}.html`,
    moduleOrder: candidate.moduleOrder,
    moduleStatus: candidate.moduleStatus,
    moduleType: candidate.moduleType,
    navTitle: candidate.navTitle,
    order: candidate.order as number,
    slug,
    sourcePath,
    summary: candidate.description ?? summarize(body),
    title: candidate.title,
  };
}

function comparePages(left: DocPage, right: DocPage): number {
  return left.order - right.order || left.title.localeCompare(right.title);
}

export const pages = Object.entries({ ...rawPages, ...apiFragments,
  "../content/reports/index.html": '<!-- burrow-doc: {"title":"Burrow Test Report","group":"Reports","order":0} -->' + report.html,
}).map(([path, raw]) => parsePage(path, raw));
const pageBySlug = new Map(pages.map((page) => [page.slug, page]));
if (pageBySlug.size !== pages.length) {
  throw new Error("docs content contains duplicate output slugs");
}
if (!pageBySlug.has("index")) {
  throw new Error("docs content must define src/content/index.html");
}

for (const area of ["book", "modules", "api"] as const) {
  const keys = new Set<string>();
  for (const page of pages.filter((candidate) => candidate.area === area)) {
    const key = `${page.group}\u0000${page.order}`;
    if (keys.has(key)) {
      throw new Error(`${page.sourcePath}: duplicate order ${page.order} in group ${page.group}`);
    }
    keys.add(key);
  }
}

export function getPage(slug: string): DocPage {
  const page = pageBySlug.get(slug);
  if (!page) {
    throw new Error(`unknown docs page: ${slug}`);
  }
  return page;
}

const BOOK_GROUPS = [
  "Contents",
  "Foundations",
  "Runtime Platform",
  "Operator Experience",
  "Module Development",
  "Engineering",
  "Reference",
];
const MODULE_ROOT_GROUP = "Modules";
for (const page of pages.filter((candidate) => candidate.area === "book")) {
  if (!BOOK_GROUPS.includes(page.group)) {
    throw new Error(`${page.sourcePath}: unknown book group ${page.group}`);
  }
}

export function navGroups(area: Exclude<DocArea, "home">): NavGroup[] {
  const grouped = new Map<string, DocPage[]>();
  for (const page of pages.filter((candidate) => candidate.area === area)) {
    const existing = grouped.get(page.group) ?? [];
    existing.push(page);
    grouped.set(page.group, existing);
  }

  const labels = [...grouped.keys()].sort((left, right) => {
    if (area === "api") return ["Overview", "Operations", "Contract"].indexOf(left) - ["Overview", "Operations", "Contract"].indexOf(right);
    if (area === "book") {
      return BOOK_GROUPS.indexOf(left) - BOOK_GROUPS.indexOf(right);
    }
    if (left === MODULE_ROOT_GROUP) return -1;
    if (right === MODULE_ROOT_GROUP) return 1;
    return left.localeCompare(right);
  });
  return labels.map((label) => ({ label, pages: grouped.get(label)!.sort(comparePages) }));
}

export function orderedPages(area: Exclude<DocArea, "home">): DocPage[] {
  return navGroups(area).flatMap((group) => group.pages);
}

export function bookParts(): BookPart[] {
  let chapterNumber = 0;
  return navGroups("book")
    .filter((group) => group.label !== "Contents")
    .map((group, partIndex) => {
      const partNumber = partIndex + 1;
      return {
        chapters: group.pages.map((page) => {
          chapterNumber += 1;
          return {
            label: String(chapterNumber).padStart(2, "0"),
            number: chapterNumber,
            page,
            partLabel: group.label,
            partNumber,
          };
        }),
        label: group.label,
        number: partNumber,
      };
    });
}

const chapterBySlug = new Map(bookParts().flatMap((part) => part.chapters).map((chapter) => [chapter.page.slug, chapter]));

export function bookChapter(page: DocPage): BookChapter | undefined {
  return chapterBySlug.get(page.slug);
}

export function rootPrefix(slug: string): string {
  return slug === "index" ? "" : "../".repeat(slug.split("/").length - 1);
}
