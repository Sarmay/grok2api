import assert from "node:assert/strict";
import { describe, it } from "node:test";

import { formatCompactTokens, formatTokenMillions } from "./format.ts";

describe("formatCompactTokens", () => {
  it("keeps small counts exact and scales larger counts by k, m, b, and t", () => {
    assert.equal(formatCompactTokens(0, "en"), "0");
    assert.equal(formatCompactTokens(999, "en"), "999");
    assert.equal(formatCompactTokens(1_000, "en"), "1k");
    assert.equal(formatCompactTokens(1_234, "en"), "1.23k");
    assert.equal(formatCompactTokens(12_345, "en"), "12.3k");
    assert.equal(formatCompactTokens(123_456, "en"), "123k");
    assert.equal(formatCompactTokens(999_500, "en"), "1m");
    assert.equal(formatCompactTokens(1_234_567, "en"), "1.23m");
    assert.equal(formatCompactTokens(12_345_678, "en"), "12.3m");
    assert.equal(formatCompactTokens(1_500_000_000, "en"), "1.5b");
    assert.equal(formatCompactTokens(2_500_000_000_000, "en"), "2.5t");
  });

  it("normalizes invalid values and keeps a negative sign", () => {
    assert.equal(formatCompactTokens(Number.NaN, "en"), "0");
    assert.equal(formatCompactTokens(-12_345, "en"), "-12.3k");
  });
});

describe("formatTokenMillions", () => {
  it("uses a stable M suffix for large values", () => {
    assert.equal(formatTokenMillions(0, "en"), "0");
    assert.equal(formatTokenMillions(999, "en"), "999");
    assert.equal(formatTokenMillions(1_000, "en"), "0.001M");
    assert.equal(formatTokenMillions(500_000, "en"), "0.5M");
    assert.equal(formatTokenMillions(1_000_000, "en"), "1M");
    assert.equal(formatTokenMillions(1_234_567, "en"), "1.23M");
  });

  it("normalizes invalid and negative values", () => {
    assert.equal(formatTokenMillions(Number.NaN, "en"), "0");
    assert.equal(formatTokenMillions(-10, "en"), "0");
  });
});
