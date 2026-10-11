// Route table. Public routes share the Layout. The admin route is lazy-loaded
// via a dynamic import so Vite emits it as a SEPARATE chunk that the visitor
// entry bundle never imports — the admin code is only fetched if someone
// actually navigates to /admin.

import { lazy, Suspense } from 'react'
import { Route, Routes } from 'react-router-dom'

import Layout from './components/Layout'
import { Loading } from './components/QueryStates'
import Home from './routes/Home'
import Works from './routes/Works'
import Contact from './routes/Contact'
import Resume from './routes/Resume'
import Blog from './routes/Blog'
import Post from './routes/Post'
import Search from './routes/Search'
import NotFound from './routes/NotFound'

// Code-split boundary: keep this a dynamic import and do not import AdminApp
// statically anywhere else, or it would be pulled into the visitor bundle.
const AdminApp = lazy(() => import('./routes/admin/AdminApp'))

export default function App() {
  return (
    <Routes>
      <Route element={<Layout />}>
        <Route path="/" element={<Home />} />
        <Route path="/works" element={<Works />} />
        <Route path="/contact" element={<Contact />} />
        <Route path="/resume" element={<Resume />} />
        <Route path="/blog" element={<Blog />} />
        <Route path="/blog/:slug" element={<Post />} />
        <Route path="/search" element={<Search />} />
        <Route path="*" element={<NotFound />} />
      </Route>
      {/* Admin is a sibling of the public Layout group so it renders in its own
          full-viewport sidebar+main shell, not the narrow public column. The
          lazy import keeps it a separate chunk the visitor entry never loads. */}
      <Route
        path="/admin/*"
        element={
          <Suspense fallback={<Loading label="Loading admin…" />}>
            <AdminApp />
          </Suspense>
        }
      />
    </Routes>
  )
}
