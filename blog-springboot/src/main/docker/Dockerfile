FROM openjdk:8
VOLUME /tmp
ADD hexo-blog-satoken-0.0.1.jar blog.jar
ENTRYPOINT ["java","-Djava.security.egd=file:/dev/./urandom","-jar","/blog.jar"]
ENV TZ=Asia/Shanghai
RUN ln -snf /usr/share/zoneinfo/$TZ /etc/localtime && echo $TZ > /etc/timezone
