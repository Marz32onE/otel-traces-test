// Replica set healthcheck: exit 0 only when rs.status().ok === 1.
// If not initiated, run rs.initiate() and exit 1 (next run will see status).
try {
  var s = rs.status();
  if (s.ok === 1) quit(0);
} catch (e) {
  try {
    rs.initiate({ _id: 'rs0', members: [{ _id: 0, host: 'mongodb:27017' }] });
  } catch (e2) {}
}
quit(1);
