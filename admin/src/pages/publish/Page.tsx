import { useState } from "react";

export const PublishPage = () => {
  const [bundleId, setBundleId] = useState("");
  const [status, setStatus] = useState("");

  const handlePublish = () => {
    if (!bundleId) {
      setStatus("Please enter a bundle ID");
      return;
    }
    // TODO: Implement actual publish API call
    setStatus(`Publishing bundle: ${bundleId}...`);
    setTimeout(() => {
      setStatus(`Bundle ${bundleId} published successfully (mock)`);
    }, 1000);
  };

  return (
    <div>
      <h2>Publish Commands</h2>
      <p>Publish command bundles to make them available for distribution.</p>
      <div style={{ marginTop: "16px", padding: "16px", border: "1px solid #ddd", borderRadius: "4px" }}>
        <div style={{ marginBottom: "16px" }}>
          <label htmlFor="bundleId" style={{ display: "block", marginBottom: "8px" }}>
            Bundle ID:
          </label>
          <input
            id="bundleId"
            type="text"
            value={bundleId}
            onChange={(e) => setBundleId(e.target.value)}
            placeholder="Enter bundle ID"
            style={{ padding: "8px", width: "300px", marginRight: "8px" }}
          />
          <button onClick={handlePublish} style={{ padding: "8px 16px" }}>
            Publish
          </button>
        </div>
        {status && (
          <div style={{ marginTop: "16px", padding: "8px", backgroundColor: "#f0f0f0", borderRadius: "4px" }}>
            {status}
          </div>
        )}
      </div>
    </div>
  );
};
