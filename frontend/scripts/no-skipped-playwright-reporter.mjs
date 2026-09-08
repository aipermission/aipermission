export default class NoSkippedPlaywrightReporter {
  constructor() {
    this.weakenedTests = new Map();
  }

  onBegin(_config, suite) {
    for (const test of suite.allTests()) {
      this.recordUnexpectedStatus(test);
    }
  }

  onTestEnd(test, result) {
    this.recordUnexpectedStatus(test);
    if (result.status === "skipped") this.weakenedTests.set(test.titlePath().join(" > "), "skipped");
  }

  onEnd(result) {
    if (this.weakenedTests.size === 0) return { status: result.status };
    const details = [...this.weakenedTests]
      .sort(([left], [right]) => left.localeCompare(right))
      .map(([title, status]) => `${title} (${status})`)
      .join("\n");
    console.error(`Skipped or expected-failure Playwright tests are forbidden:\n${details}`);
    return { status: "failed" };
  }

  recordUnexpectedStatus(test) {
    if (test.expectedStatus !== "passed") {
      this.weakenedTests.set(test.titlePath().join(" > "), test.expectedStatus);
    }
  }
}
