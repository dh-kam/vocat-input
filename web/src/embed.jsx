// Remote entry for Module Federation. Importing this module from the host
// loads vocat's scoped embed CSS (no Tailwind preflight, .vocat-shell-scoped)
// as a side effect, then re-exports the App component. The host passes
// `embedded` and `apiBase` props.
import './embed.css';
import App from './App.jsx';

export default App;
