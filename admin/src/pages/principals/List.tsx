import { useList } from "@refinedev/core";

export const PrincipalsList = () => {
  const { data, isLoading } = useList({
    resource: "principals",
  });

  if (isLoading) {
    return <div>Loading principals...</div>;
  }

  return (
    <div>
      <h2>Principals</h2>
      <p>Manage users, teams, and their permissions.</p>
      <div style={{ marginTop: "16px", padding: "16px", border: "1px solid #ddd", borderRadius: "4px" }}>
        {data?.data?.length ? (
          <ul>
            {data.data.map((principal: any) => (
              <li key={principal.id}>{principal.name || principal.id}</li>
            ))}
          </ul>
        ) : (
          <p>No principals configured yet.</p>
        )}
      </div>
    </div>
  );
};
