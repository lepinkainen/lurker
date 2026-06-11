import { beforeEach } from "vitest";

// Browser-mode tests share one origin, so localStorage persists across test
// files. Clear it before every test to stop state leaking between files.
beforeEach(() => {
  localStorage.clear();
});
