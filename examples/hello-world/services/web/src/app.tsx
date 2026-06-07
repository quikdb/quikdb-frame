import { useState, useEffect } from 'preact/hooks';

interface Task {
  id: string;
  title: string;
  done: boolean;
}

const API_URL = import.meta.env.VITE_API_URL || 'http://localhost:8080';

export function App() {
  const [tasks, setTasks] = useState<Task[]>([]);
  const [message, setMessage] = useState('Loading...');

  useEffect(() => {
    fetch(`${API_URL}/api/hello`)
      .then(r => r.json())
      .then(data => setMessage(data.message))
      .catch(() => setMessage('Could not connect to API'));

    fetch(`${API_URL}/api/tasks`)
      .then(r => r.json())
      .then(data => setTasks(data))
      .catch(() => {});
  }, []);

  return (
    <div style={{ maxWidth: '600px', margin: '40px auto', fontFamily: 'system-ui, sans-serif', padding: '0 20px' }}>
      <h1 style={{ fontSize: '24px', marginBottom: '8px' }}>quikdb-frame</h1>
      <p style={{ color: '#666', marginBottom: '32px' }}>{message}</p>

      <h2 style={{ fontSize: '18px', marginBottom: '16px' }}>Tasks</h2>
      <ul style={{ listStyle: 'none', padding: 0 }}>
        {tasks.map(task => (
          <li key={task.id} style={{
            padding: '12px 16px',
            borderBottom: '1px solid #eee',
            display: 'flex',
            alignItems: 'center',
            gap: '12px',
          }}>
            <input type="checkbox" checked={task.done} readOnly />
            <span>{task.title}</span>
          </li>
        ))}
      </ul>

      <p style={{ marginTop: '32px', fontSize: '13px', color: '#999' }}>
        API: {API_URL} | Built with quikdb-frame
      </p>
    </div>
  );
}
