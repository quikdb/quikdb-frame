const express = require("express");
const app = express();
app.use("/assets", express.static("public"));
app.get("/health", (_req, res) => res.json({"status":"ok","service":"catalog"}));
app.get("/about", (_req, res) => res.type("text/plain").send("QuikDB conversion fixture"));
app.post("/api/orders", (_req, res) => res.status(201).json({"accepted":true,"code":"fixture-order"}));
const port = Number(process.env.PORT || 3187);
app.listen(port);
