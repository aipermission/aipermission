function isTestSource(classifier, file, testModuleMarkers) {
  if (classifier === "go") return file.endsWith("_test.go");
  if (classifier === "markers") {
    return testModuleMarkers.some((marker) => file.includes(marker));
  }
  if (classifier === "all") return true;
  throw new Error(`unknown maintenance source classifier: ${classifier}`);
}

module.exports = { isTestSource };
