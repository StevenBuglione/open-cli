import { useList } from "@refinedev/core";

export const BundlesList = () => {
  const { data, isLoading } = useList({
    resource: "bundles",
  });

  if (isLoading) {
    return <div>Loading bundles...</div>;
  }

  return (
    <div>
      <h2>Bundles</h2>
      <p>View and manage command bundles available for distribution.</p>
      <div style={{ marginTop: "16px", padding: "16px", border: "1px solid #ddd", borderRadius: "4px" }}>
        {data?.data?.length ? (
          <ul>
            {data.data.map((bundle: any) => (
              <li key={bundle.id}>{bundle.name || bundle.id}</li>
            ))}
          </ul>
        ) : (
          <p>No bundles available yet.</p>
        )}
      </div>
    </div>
  );
};
