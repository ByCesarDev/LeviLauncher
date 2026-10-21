import { ListDir } from "bindings/github.com/liteldev/LeviLauncher/internal/app/minecraft";

export type DirEntry = { name: string; path: string };

export function getPathBaseName(path: unknown): string {
  const value = String(path || "").trim();
  if (!value) return "";
  const parts = value.replace(/\\/g, "/").split("/").filter(Boolean);
  return parts.at(-1) || value;
}

export function normalizeDroppedFiles(files: unknown): string[] {
  if (!Array.isArray(files)) return [];

  return files
    .map((item) => (typeof item === "string" ? item.trim() : ""))
    .filter(Boolean);
}

export async function listDirectories(path: string, throwOnError = false): Promise<DirEntry[]> {
  try {
    const list = await ListDir(path);
    return (list || [])
      .filter((e: any) => e.isDir)
      .map((e: any) => ({ name: e.name, path: e.path }));
  } catch (error) {
    if (throwOnError) throw error;
    return [];
  }
}

export async function countDirectories(path: string | undefined | null, countArchives = false): Promise<number> {
  const p = String(path || "").trim();
  if (!p) return 0;
  try {
    const list = await ListDir(p);
    return (list || []).filter((e: any) => {
      if (e.isDir) return true;
      if (countArchives) {
        const lower = String(e.name || "").toLowerCase();
        return lower.endsWith(".mcpack") || lower.endsWith(".zip");
      }
      return false;
    }).length;
  } catch {
    return 0;
  }
}
