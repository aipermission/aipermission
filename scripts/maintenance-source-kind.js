function isTestSource(directory, file, frontendTestModuleMarkers) {
  if (directory === "backend") {
    return file.endsWith("_test.go");
  }
  if (directory === "frontend/src") {
    return frontendTestModuleMarkers.some((marker) => file.includes(marker));
  }
  if (directory === "packages/mcp/src") {
    return /(?:\.test\.|\.spec\.)/.test(file);
  }
  throw new Error(`unknown maintenance source directory: ${directory}`);
}

module.exports = { isTestSource };
