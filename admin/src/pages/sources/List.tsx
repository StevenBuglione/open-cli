import { useList } from "@refinedev/core";

export const SourcesList = () => {
  const { data, isLoading } = useList({
    resource: "sources",
  });

  if (isLoading) {
    return <div>Loading sources...</div>;
  }

  return (
    <div>
      <h2>Sources</h2>
      <p>Manage command sources and their configurations.</p>
      <div style={{ marginTop: "16px", padding: "16px", border: "1px solid #ddd", borderRadius: "4px" }}>
        {data?.data?.length ? (
          <ul>
            {data.data.map((source: any) => (
              <li key={source.id}>{source.name || source.id}</li>
            ))}
          </ul>
        ) : (
          <p>No sources configured yet.</p>
        )}
      </div>
    </div>
  );
};
