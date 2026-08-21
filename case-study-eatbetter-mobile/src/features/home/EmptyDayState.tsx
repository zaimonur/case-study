import { StyleSheet, Text, View } from 'react-native';

export function EmptyDayState() {
  return (
    <View style={styles.card}>
      <Text style={styles.title}>Bugün henüz öğün eklenmedi.</Text>
      <Text style={styles.description}>İlk öğününüzü eklemek için yiyecek arayabilirsiniz.</Text>
    </View>
  );
}

const styles = StyleSheet.create({
  card: {
    borderWidth: 1,
    borderColor: '#dce7e1',
    borderRadius: 16,
    padding: 20,
    backgroundColor: '#ffffff',
  },
  title: { color: '#1d2b26', fontSize: 16, fontWeight: '700' },
  description: { marginTop: 7, color: '#64716c', fontSize: 14, lineHeight: 20 },
});
