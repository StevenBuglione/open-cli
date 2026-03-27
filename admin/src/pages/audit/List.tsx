import { useList } from "@refinedev/core";

export const AuditList = () => {
  const { data, isLoading } = useList({
    resource: "audit",
  });

  if (isLoading) {
    return <div>Loading audit logs...</div>;
  }

  return (
    <div>
      <h2>Audit Log</h2>
      <p>View audit trail of administrative actions and changes.</p>
      <div style={{ marginTop: "16px", padding: "16px", border: "1px solid #ddd", borderRadius: "4px" }}>
        {data?.data?.length ? (
          <table style={{ width: "100%", borderCollapse: "collapse" }}>
            <thead>
              <tr style={{ borderBottom: "2px solid #ddd", textAlign: "left" }}>
                <th style={{ padding: "8px" }}>Timestamp</th>
                <th style={{ padding: "8px" }}>Action</th>
                <th style={{ padding: "8px" }}>User</th>
                <th style={{ padding: "8px" }}>Resource</th>
              </tr>
            </thead>
            <tbody>
              {data.data.map((log: any) => (
                <tr key={log.id} style={{ borderBottom: "1px solid #eee" }}>
                  <td style={{ padding: "8px" }}>{log.timestamp}</td>
                  <td style={{ padding: "8px" }}>{log.action}</td>
                  <td style={{ padding: "8px" }}>{log.user}</td>
                  <td style={{ padding: "8px" }}>{log.resource}</td>
                </tr>
              ))}
            </tbody>
          </table>
        ) : (
          <p>No audit logs available yet.</p>
        )}
      </div>
    </div>
  );
};
