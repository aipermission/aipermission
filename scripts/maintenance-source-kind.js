function isTestSource(classifier, file, testModuleMarkers) {
  if (classifier === "go") return file.endsWith("_test.go");
  if (classifier === "markers") {
    const normalized = String(file).replaceAll("\\", "/");
    const filename = normalized.slice(normalized.lastIndexOf("/") + 1);
    const extensionIndex = filename.lastIndexOf(".");
    if (extensionIndex < 0) return false;
    const extension = filename.slice(extensionIndex);
    return testModuleMarkers.some((marker) =>
      filename.endsWith(`${marker.slice(0, -1)}${extension}`),
    );
  }
  if (classifier === "all") return true;
  throw new Error(`unknown maintenance source classifier: ${classifier}`);
}

module.exports = { isTestSource };
