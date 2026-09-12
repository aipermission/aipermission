function isTestSource(classifier, file, testModuleMarkers) {
  if (classifier === "go") return file.endsWith("_test.go");
  if (classifier === "markers") {
    const normalized = String(file).replaceAll("\\", "/");
    const filename = normalized.slice(normalized.lastIndexOf("/") + 1);
    return testModuleMarkers.some((marker) => filename.includes(marker));
  }
  if (classifier === "all") return true;
  throw new Error(`unknown maintenance source classifier: ${classifier}`);
}

module.exports = { isTestSource };
