module.exports = {
  content: ["./internal/web/static/**/*.{html,js}"],
  theme: { extend: {} },
  important: "body",
  plugins: [],
  output: "@internal/web/static/css/tailwind.css",
  minify: true,
}
