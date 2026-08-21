import { StyleSheet, Text, View } from 'react-native';

export default function SearchScreen() {
  return (
    <View style={styles.container}>
      <Text style={styles.title}>Yiyecek Ara</Text>
      <Text style={styles.description}>Yiyecek arama sonraki görevde eklenecek.</Text>
    </View>
  );
}

const styles = StyleSheet.create({
  container: { flex: 1, justifyContent: 'center', padding: 28, backgroundColor: '#f7faf8' },
  title: { color: '#1d2b26', fontSize: 28, fontWeight: '700' },
  description: { marginTop: 10, color: '#64716c', fontSize: 16, lineHeight: 24 },
});
