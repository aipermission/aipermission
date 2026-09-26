export function isNodeUnitTest(name) {
  return /\.test\.(js|ts)$/.test(name) && !/\.(component|type)\.test\.(js|ts)$/.test(name);
}
