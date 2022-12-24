package org.cuit.epoch;

import org.cuit.epoch.entity.Comment;
import org.cuit.epoch.exception.AppException;
import org.cuit.epoch.mapper.CommentMapper;
import org.cuit.epoch.util.PasswordUtils;
import org.junit.jupiter.api.Test;
import org.springframework.beans.factory.annotation.Autowired;
import org.springframework.boot.test.context.SpringBootTest;

import java.util.ArrayList;
import java.util.List;

@SpringBootTest
class HexoBlogSatokenApplicationTests {

    @Autowired
    CommentMapper commentMapper;



//    @Autowired
//    private ArticleSearchRepository elasticsearchDao;
//
//
//    @Test
//    void test122(){
//        // 获取文章数据
//        Article article = Article.builder().articleTitle("test1中的es数据插入1").articleContent("nihaonihao你好这里是小小的博客").build();
//        elasticsearchDao.save(BeanCopyUtils.copyObject(article, ArticleSearchDTO.class));
//
//    }

    @Test
    void contextLoads() {
    }

    @Test
    void getEncodePassword() {
        System.out.println(PasswordUtils.encrypt("nihao123"));
    }


    @Test
    void test() {
        test1();
    }

    void test1() {
        try {
            throw new AppException("这里是异常1");
        } finally {
            throw new AppException("这里是异常2");
        }
    }


    @Test
    void testMailMessage(){
        ArrayList<Integer> ids = new ArrayList<>();
        ids.add(767);
        ids.add(766);
        ids.add(768);
        ids.add(769);
        ids.add(772);

        List<Comment> comments = commentMapper.listAllCommentsByIds(ids);
        for (Comment comment : comments) {
            System.out.println("comment = " + comment);
        }
    }


}
