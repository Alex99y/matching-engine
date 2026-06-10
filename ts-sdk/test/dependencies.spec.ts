import { readFileSync } from "node:fs";
import { describe, expect, it } from "vitest";

// Enforces the "empty runtime dependencies" rule: an SDK installed in other
// people's projects must rely only on native platform APIs.
describe("runtime dependencies", () => {
  it("declares no runtime dependencies", () => {
    const pkg = JSON.parse(
      readFileSync(new URL("../package.json", import.meta.url), "utf8"),
    ) as { dependencies?: Record<string, string> };
    expect(Object.keys(pkg.dependencies ?? {})).toHaveLength(0);
  });
});
