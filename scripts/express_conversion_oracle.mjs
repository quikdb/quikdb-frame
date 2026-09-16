const [originalBase, convertedBase] = process.argv.slice(2);

if (!originalBase || !convertedBase) {
  throw new Error("usage: node scripts/express_conversion_oracle.mjs <original-url> <converted-url>");
}

const requests = [
  { method: "GET", path: "/health", compareContentType: true },
  { method: "GET", path: "/about", compareContentType: true },
  { method: "POST", path: "/api/orders", compareContentType: true },
  { method: "GET", path: "/assets/banner.txt", compareContentType: false },
  { method: "GET", path: "/assets/app.js", compareContentType: false },
];

function normalizedContentType(value) {
  return (value || "").toLowerCase().replace(/\s+/g, "");
}

async function observe(base, request) {
  const response = await fetch(base + request.path, { method: request.method });
  return {
    status: response.status,
    contentType: normalizedContentType(response.headers.get("content-type")),
    body: await response.text(),
  };
}

const results = [];
for (const request of requests) {
  const [original, converted] = await Promise.all([
    observe(originalBase, request),
    observe(convertedBase, request),
  ]);
  const comparableOriginal = request.compareContentType
    ? original
    : { status: original.status, body: original.body };
  const comparableConverted = request.compareContentType
    ? converted
    : { status: converted.status, body: converted.body };
  if (JSON.stringify(comparableOriginal) !== JSON.stringify(comparableConverted)) {
    throw new Error(`${request.method} ${request.path} differs:\noriginal=${JSON.stringify(original)}\nconverted=${JSON.stringify(converted)}`);
  }
  results.push({ method: request.method, path: request.path, ...original });
}

process.stdout.write(JSON.stringify({ parity: true, requests: results }, null, 2) + "\n");
